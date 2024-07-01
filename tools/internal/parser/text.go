package parser

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	xunicode "golang.org/x/text/encoding/unicode"
)

// Source is a piece of source text with location information.
//
// A Source is effectively a slice of the input file's lines, with
// some extra information attached. As such, the start/end indexes
// behave the same as in Go slices, and select the half-open interval
// [start:end).
type Source struct {
	// The lines of source text, sanitized to valid UTF-8 and with
	// leading and trailing whitespace removed.
	lines []string
	// lineOffset is how many lines are before the beginning of lines,
	// for sources that represent a subset of the input.
	lineOffset int
}

// newSource returns a source for bs, along with a preliminary set of
// input validation errors.
//
// source always returns a usable, non-nil result, even when it
// returns errors.
func newSource(bs []byte) (Source, []error) {
	lines, errs := normalizeToUTF8Lines(bs)

	ret := Source{
		lines:      lines,
		lineOffset: 0,
	}

	return ret, errs
}

// Text returns the source text of s as a string.
func (s Source) Text() string {
	if len(s.lines) == 1 {
		return s.lines[0]
	}
	return strings.Join(s.lines, "\n")
}

func (s Source) NumLines() int {
	return len(s.lines)
}

// LocationString returns a short string describing the source
// location.
func (s Source) LocationString() string {
	// For printing diagnostics, 0-indexed [start:end) is confusing
	// and not how editors present text to people. Adjust the offsets
	// to be 1-indexed [start:end] instead.
	start := s.lineOffset + 1
	end := s.lineOffset + len(s.lines)

	if end < start {
		// Zero line Source. We can sometimes produce these internally
		// during parsing, but they should not escape outside the
		// package. We still print them gracefully instead of
		// panicking, because it's useful for debugging the parser.
		return fmt.Sprintf("<invalid Source, 0-line range before line %d>", start)
	}

	if start == end {
		return fmt.Sprintf("line %d", start)
	}
	return fmt.Sprintf("lines %d-%d", start, end)
}

// slice returns the slice of s between startLine and endLine.
//
// startLine and endLine behave like normal slice offsets, i.e. they
// represent the half-open range [startLine:endLine).
func (s Source) slice(startLine, endLine int) Source {
	if startLine < 0 || startLine > len(s.lines) || endLine < startLine || endLine > len(s.lines) {
		panic("invalid input to slice")
	}
	return Source{
		lines:      s.lines[startLine:endLine],
		lineOffset: s.lineOffset + startLine,
	}
}

// first returns the first line of s.
// If s is empty, first panics.
func (s Source) first() Source {
	return s.slice(0, 1)
}

// XXXXX needed?
// // last returns the last line of s.
// // If s is empty, last panics.
// func (s Source) last() Source {
// 	return s.slice(len(s.lines)-1, len(s.lines))
// }

// empty reports whether s contains any lines.
func (s Source) empty() bool {
	return len(s.lines) == 0
}

// takeN removes n lines from s and returns them as a new Source.
// If s contains fewer than n lines, takeN panics.
func (s *Source) takeN(n int) Source {
	ret := Source{
		lines:      s.lines[:n],
		lineOffset: s.lineOffset,
	}
	s.lines = s.lines[n:]
	s.lineOffset += n
	return ret
}

// takeOne removes one line from s and returns it as a new Source.
// if s is empty, takeOne panics.
func (s *Source) takeOne() Source {
	return s.takeN(1)
}

// takeWhile removes the (possibly empty) run of initial lines from s
// where fn(line) is true, and returns them as a new Source.
func (s *Source) takeWhile(fn func(string) bool) Source {
	for i, ln := range s.lines {
		if !fn(ln) {
			return s.takeN(i)
		}
	}

	// No match, take all.
	return s.takeN(len(s.lines))
}

func (s *Source) takeWhileNot(fn func(string) bool) Source {
	return s.takeWhile(func(s string) bool { return !fn(s) })
}

// append extends s with the lines of o.
// The two Sources must directly follow each other in the original
// source text. If s and o do not represent a single continuous run of
// lines, append panics.
func (s *Source) append(o Source) {
	if s.lineOffset+s.NumLines() != o.lineOffset {
		panic("invalid append of non-adjacent Sources")
	}
	// Go slice/capacity semantics are a footgun here: we construct
	// Sources by taking sub-slices of the overall input, which menas
	// a lot of these slices have extra capacity, which contain later
	// lines of the original input. If we just append naively here, we
	// might overwrite those extra lines and corrupt the input.
	//
	// To avoid this, make an explicit copy here.
	newLines := make([]string, 0, len(s.lines)+len(o.lines))
	newLines = append(newLines, s.lines...)
	newLines = append(newLines, o.lines...)
	s.lines = newLines
}

const (
	bomUTF8    = "\xEF\xBB\xBF"
	bomUTF16BE = "\xFE\xFF"
	bomUTF16LE = "\xFF\xFE"
)

// The transformers that normalizeToUTF8Lines can use to process input
// into valid UTF-8, and that guessUTFVariant can return.
var (
	utf8Transform              = xunicode.UTF8BOM
	utf16LittleEndianTransform = xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM)
	utf16BigEndianTransform    = xunicode.UTF16(xunicode.BigEndian, xunicode.UseBOM)
)

// normalizeToUTF8Lines slices bs into one string per line.
//
// All returned strings contain only valid UTF-8. Invalid byte
// sequences are replaced with the unicode replacement character
// (\uFFFD).
//
// The canonical PSL encoding is a file consisting entirely of valid
// UTF-8, with no leading BOM or unicode replacement characters. In an
// effort to report useful errors for common mangling caused by older
// Windows software, normalizeToUTF8Lines accepts input encoded as
// UTF-8, UTF-16LE or UTF-16BE, with or without a leading BOM.
//
// normalizeToUTF8Lines returns the normalized lines of bs, as well as
// errors that report deviations from the canonical encoding, if any.
func normalizeToUTF8Lines(bs []byte) ([]string, []error) {
	var errs []error

	enc := utf8Transform
	switch {
	case bytes.HasPrefix(bs, []byte(bomUTF8)):
		errs = append(errs, UTF8BOMError{})
	case bytes.HasPrefix(bs, []byte(bomUTF16BE)):
		enc = utf16BigEndianTransform
		errs = append(errs, InvalidEncodingError{"UTF-16BE"})
	case bytes.HasPrefix(bs, []byte(bomUTF16LE)):
		enc = utf16LittleEndianTransform
		errs = append(errs, InvalidEncodingError{"UTF-16LE"})
	default:
		enc = guessUTFVariant(bs)
		switch enc {
		case utf16BigEndianTransform:
			errs = append(errs, InvalidEncodingError{"UTF-16BE (guessed)"})
		case utf16LittleEndianTransform:
			errs = append(errs, InvalidEncodingError{"UTF-16LE (guessed)"})
		}
	}

	bs, err := enc.NewDecoder().Bytes(bs)
	if err != nil {
		// The decoder shouldn't error out, if it does we can't really
		// proceed, just return the errors we've found so far.
		errs = append(errs, err)
		return []string{}, errs
	}

	if len(bs) == 0 {
		return []string{}, errs
	}

	ret := strings.Split(string(bs), "\n")
	for i, line := range ret {
		// capture source info before we tidy up the line starts/ends,
		// so that input normalization errors show the problem being
		// described.
		//
		// However, we still provide post-sanitization UTF-8 bytes,
		// not the raw input. The raw input is unlikely to display
		// correctly in terminals and logs, and because the unicode
		// replacement character is a distinctive shape that stands
		// out, it should provide enough hints as to where any invalid
		// byte sequences are.
		src := Source{
			lineOffset: i,
			lines:      []string{line},
		}
		if strings.ContainsRune(line, utf8.RuneError) {
			errs = append(errs, InvalidUTF8Error{src})
		}
		line, ok := strings.CutSuffix(line, "\r")
		if ok {
			ret[i] = line
			errs = append(errs, DOSNewlineError{src})
		}
		if ln := strings.TrimRightFunc(line, unicode.IsSpace); ln != line {
			line = ln
			ret[i] = line
			errs = append(errs, TrailingWhitespaceError{src})
		}
		if ln := strings.TrimLeftFunc(line, unicode.IsSpace); ln != line {
			line = ln
			ret[i] = line
			errs = append(errs, LeadingWhitespaceError{src})
		}
	}

	return ret, errs
}

// guessUTFVariant guesses the encoding of bs.
//
// Returns the transformer to use on bs, one of utf8Transform,
// utf16LittleEndianTransform or utf16BigEndianTransform.
func guessUTFVariant(bs []byte) encoding.Encoding {
	// Only scan a few hundred bytes. Assume UTF-8 if we don't see
	// anything odd before that.
	const checkLimit = 200 // 100 UTF-16 characters
	if len(bs) > checkLimit {
		bs = bs[:checkLimit]
	}

	// This is a crude but effective trick to detect UTF-16: we assume
	// that the input contains at least some ascii, and that the
	// decoded input does not contain Unicode \u0000 codepoints
	// (legacy ascii null).
	//
	// If this is true, then valid UTF-8 text does not have any zero
	// bytes, because UTF-8 never produces a zero byte except when it
	// encodes the \u0000 codepoint.
	//
	// On the other hand, UTF-16 encodes all codepoints a pair of
	// bytes, and that means an ascii string in UTF-16 a zero byte
	// every 2 bytes. We can use the presence of zero bytes to
	// identify UTF-16, and the position of the zero (even or odd
	// offset) tells us what endianness to use.
	evenZeros, oddZeros := 0, 0
	for i, b := range bs {
		if b != 0 {
			continue
		}

		if i%2 == 0 {
			evenZeros++
		} else {
			oddZeros++
		}

		const (
			// Wait for a few zero bytes to accumulate, because if
			// this is just UTF-8 with a few \u0000 codepoints,
			// decoding as UTF-16 will be complete garbage. So, wait
			// until we see a suspicious number of zeros, and require
			// a strong bias towards even/odd before we guess
			// UTF-16. Otherwise, UTF-8 gives us the best chance of
			// producing coherent errors.
			decisionThreshold = 20
			utf16Threshold    = 15
		)
		if evenZeros+oddZeros < decisionThreshold {
			continue
		}
		if evenZeros > utf16Threshold {
			return utf16BigEndianTransform
		} else if oddZeros > utf16Threshold {
			return utf16LittleEndianTransform
		}
		// Lots of zeros, but no strong bias. No idea what's going on,
		// UTF-8 is a safe fallback.
		return utf8Transform
	}

	// Didn't find enough zeros, probably UTF-8.
	return utf8Transform
}
