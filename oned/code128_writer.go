package oned

import (
	"github.com/makiuchi-d/gozxing"
)

// This object renders a CODE128 code as a {@link BitMatrix}.

const (
	// Dummy characters used to specify control characters in input
	code128ESCAPE_FNC_1 = '\u00f1'
	code128ESCAPE_FNC_2 = '\u00f2'
	code128ESCAPE_FNC_3 = '\u00f3'
	code128ESCAPE_FNC_4 = '\u00f4'
)

// Results of minimal lookahead for code C
type code128CType int

const (
	code128CType_UNCODABLE = code128CType(iota)
	code128CType_ONE_DIGIT
	code128CType_TWO_DIGITS
	code128CType_FNC_1
)

func (ctype code128CType) String() string {
	switch ctype {
	case code128CType_UNCODABLE:
		return "UNCODABLE"
	case code128CType_ONE_DIGIT:
		return "ONE_DIGIT"
	case code128CType_TWO_DIGITS:
		return "TWO_DIGITS"
	case code128CType_FNC_1:
		return "FNC_1"
	}
	return ""
}

type code128Encoder struct{}

func NewCode128Writer() gozxing.Writer {
	return NewOneDimensionalCodeWriter(code128Encoder{}, gozxing.BarcodeFormat_CODE_128)
}

func (code128Encoder) encode(contents string) ([]bool, error) {
	length := len(contents)
	// Check length
	if length < 1 || length > 80 {
		return nil, gozxing.NewWriterException("IllegalArgumentException: "+
			"Contents length should be between 1 and 80 characters, but got %v", length)
	}
	// Check content
	for i := 0; i < length; i++ {
		c := contents[i]
		switch c {
		case code128ESCAPE_FNC_1, code128ESCAPE_FNC_2, code128ESCAPE_FNC_3, code128ESCAPE_FNC_4:
			break
		default:
			if c > 127 {
				// support for FNC4 isn't implemented, no full Latin-1 character set available at the moment
				return nil, gozxing.NewWriterException(
					"IllegalArgumentException: Bad character in input: %v", c)
			}
		}
	}

	patterns := make([][]int, 0) // temporary storage for patterns
	checkSum := 0
	checkWeight := 1
	codeSet := 0  // selected code (CODE_CODE_B or CODE_CODE_C)
	position := 0 // position in contents

	for position < length {
		//Select code to use
		newCodeSet := code128ChooseCode(contents, position, codeSet)

		//Get the pattern index
		var patternIndex int
		if newCodeSet == codeSet {
			// Encode the current character
			// First handle escapes
			switch contents[position] {
			case code128ESCAPE_FNC_1:
				patternIndex = code128CODE_FNC_1
				break
			case code128ESCAPE_FNC_2:
				patternIndex = code128CODE_FNC_2
				break
			case code128ESCAPE_FNC_3:
				patternIndex = code128CODE_FNC_3
				break
			case code128ESCAPE_FNC_4:
				if codeSet == code128CODE_CODE_A {
					patternIndex = code128CODE_FNC_4_A
				} else {
					patternIndex = code128CODE_FNC_4_B
				}
				break
			default:
				// Then handle normal characters otherwise
				switch codeSet {
				case code128CODE_CODE_A:
					patternIndex = int(contents[position]) - ' '
					if patternIndex < 0 {
						// everything below a space character comes behind the underscore in the code patterns table
						patternIndex += '`'
					}
					break
				case code128CODE_CODE_B:
					patternIndex = int(contents[position]) - ' '
					break
				default:
					// CODE_CODE_C
					patternIndex = (int(contents[position])-'0')*10 + (int(contents[position+1]) - '0')
					position++ // Also incremented below
					break
				}
			}
			position++
		} else {
			// Should we change the current code?
			// Do we have a code set?
			if codeSet == 0 {
				// No, we don't have a code set
				switch newCodeSet {
				case code128CODE_CODE_A:
					patternIndex = code128CODE_START_A
					break
				case code128CODE_CODE_B:
					patternIndex = code128CODE_START_B
					break
				default:
					patternIndex = code128CODE_START_C
					break
				}
			} else {
				// Yes, we have a code set
				patternIndex = newCodeSet
			}
			codeSet = newCodeSet
		}

		// Get the pattern
		patterns = append(patterns, code128CODE_PATTERNS[patternIndex])

		// Compute checksum
		checkSum += patternIndex * checkWeight
		if position != 0 {
			checkWeight++
		}
	}

	// Compute and append checksum
	checkSum %= 103
	patterns = append(patterns, code128CODE_PATTERNS[checkSum])

	// Append stop code
	patterns = append(patterns, code128CODE_PATTERNS[code128CODE_STOP])

	// Compute code width
	codeWidth := 0
	for _, pattern := range patterns {
		for _, width := range pattern {
			codeWidth += width
		}
	}

	// Compute result
	result := make([]bool, codeWidth)
	pos := 0
	for _, pattern := range patterns {
		pos += onedWriter_appendPattern(result, pos, pattern, true)
	}

	return result, nil
}

func code128FindCType(value string, start int) code128CType {
	last := len(value)
	if start >= last {
		return code128CType_UNCODABLE
	}
	c := value[start]
	if c == code128ESCAPE_FNC_1 {
		return code128CType_FNC_1
	}
	if c < '0' || c > '9' {
		return code128CType_UNCODABLE
	}
	if start+1 >= last {
		return code128CType_ONE_DIGIT
	}
	c = value[start+1]
	if c < '0' || c > '9' {
		return code128CType_ONE_DIGIT
	}
	return code128CType_TWO_DIGITS
}

func code128ChooseCode(value string, start, oldCode int) int {
	lookahead := code128FindCType(value, start)
	if lookahead == code128CType_ONE_DIGIT {
		if oldCode == code128CODE_CODE_A {
			return code128CODE_CODE_A
		}
		return code128CODE_CODE_B
	}
	if lookahead == code128CType_UNCODABLE {
		if start < len(value) {
			c := value[start]
			if c < ' ' || (oldCode == code128CODE_CODE_A && (c < '`' ||
				(c >= code128ESCAPE_FNC_1 && c <= code128ESCAPE_FNC_4))) {
				// can continue in code A, encodes ASCII 0 to 95 or FNC1 to FNC4
				return code128CODE_CODE_A
			}
		}
		return code128CODE_CODE_B // no choice
	}
	if oldCode == code128CODE_CODE_A && lookahead == code128CType_FNC_1 {
		return code128CODE_CODE_A
	}
	if oldCode == code128CODE_CODE_C { // can continue in code C
		return code128CODE_CODE_C
	}
	if oldCode == code128CODE_CODE_B {
		if lookahead == code128CType_FNC_1 {
			return code128CODE_CODE_B // can continue in code B
		}
		// Seen two consecutive digits, see what follows
		lookahead = code128FindCType(value, start+2)
		if lookahead == code128CType_UNCODABLE || lookahead == code128CType_ONE_DIGIT {
			return code128CODE_CODE_B // not worth switching now
		}
		if lookahead == code128CType_FNC_1 { // two digits, then FNC_1...
			lookahead = code128FindCType(value, start+3)
			if lookahead == code128CType_TWO_DIGITS { // then two more digits, switch
				return code128CODE_CODE_C
			} else {
				return code128CODE_CODE_B // otherwise not worth switching
			}
		}
		// At this point, there are at least 4 consecutive digits.
		// Look ahead to choose whether to switch now or on the next round.
		index := start + 4
		for {
			lookahead = code128FindCType(value, index)
			if lookahead != code128CType_TWO_DIGITS {
				break
			}
			index += 2
		}
		if lookahead == code128CType_ONE_DIGIT { // odd number of digits, switch later
			return code128CODE_CODE_B
		}
		return code128CODE_CODE_C // even number of digits, switch now
	}
	// Here oldCode == 0, which means we are choosing the initial code
	if lookahead == code128CType_FNC_1 { // ignore FNC_1
		lookahead = code128FindCType(value, start+1)
	}
	if lookahead == code128CType_TWO_DIGITS { // at least two digits, start in code C
		return code128CODE_CODE_C
	}
	return code128CODE_CODE_B
}