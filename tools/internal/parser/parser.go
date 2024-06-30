// Package parser implements a validating parser for the PSL files.
package parser

import (
	"slices"
	"strings"
)

// Parse parses bs as a PSL file and returns the parse result.
//
// The parser tries to keep going when it encounters errors. Parse and
// validation errors are accumulated in the Errors field of the
// returned File.
//
// If the returned File has a non-empty Errors field, the parsed file
// does not comply with the PSL format (documented at
// https://github.com/publicsuffix/list/wiki/Format), or with PSL
// submission guidelines
// (https://github.com/publicsuffix/list/wiki/Guidelines). A File with
// errors should not be used to calculate public suffixes for FQDNs.
func Parse(bs []byte) *File {
	src, srcErrs := newSource(bs)

	p := parser{
		state: state{
			Source: src,
		},
		errs: srcErrs,
	}
	p.parse()

	return &File{
		Blocks: p.blocks,
		Errors: p.errs,
	}
}

type parser struct {
	state

	// saved from outer parsers
	stack []state

	errs []error
}

type state struct {
	Source
	blocks []Block
}

const (
	sectionMarkerPrefix = "// ==="
	commentPrefix       = "// "
)

func (p *parser) parse() {
	for p.parseWhitespace() {
		next := p.first().Text()
		switch {
		case isPotentialSectionMarker(next):
			p.parseSection()
		case isComment(next):
			p.parseComment()
		default:
			p.parseInvalid()
		}
	}
}

func (p *parser) parseSection() {
	name, ok := p.checkSectionStart(p.first())
	if !ok {
		p.parseInvalid()
		return
	}

	isSectionEnd := func(s string) bool {
		if !isPotentialSectionMarker(s) {
			return false
		}
		kind, endName, _ := parseSectionMarker(s)
		if kind != "END" || endName != name {
			return false
		}
		return true
	}
	src, hasEnd := p.takeUntil(isSectionEnd)

	section := &Section{
		Source: src,
		Name:   name,
	}
	p.addBlock(section)
	p.pushState(section.Source)
	defer func() {
		section.Blocks = p.popState()
	}()

	if hasEnd {
		if _, _, missingTerminator := parseSectionMarker(src.last().Text()); missingTerminator {
			p.addError(UnterminatedSectionMarker{src.last()})
		}
	} else {
		p.addError(UnclosedSectionError{section})
	}

	for p.parseWhitespace() {
		next := p.first().Text()
		switch {
		case isPotentialSectionMarker(next):
			p.parseRogueSection(section)
		case isStartOfGroup(next):
			p.parseGroup()
		default:
			p.parseSuffixBlock()
		}
	}
}

func (p *parser) parseGroup() {
	src, hasEnd := p.takeUntil(isPotentialEndOfGroup)

	group := &Group{
		Source: src,
		Name:   "Amazon",
	}
	p.addBlock(group)
	p.pushState(group.Source)
	defer func() {
		group.Blocks = p.popState()
	}()

	if hasEnd {
		// XXXXXXX
	} else {
		// XXXXXXX
	}

	for p.parseWhitespace() {
		next := p.first().Text()
		switch {
		case isPotentialSectionMarker(next):
		// XXXXXXX something
		case isStartOfGroup(next):
		// XXXXXXX error
		default:
			p.parseSuffixBlock()
		}
	}
}

func (p *parser) checkSectionStart(src Source) (name string, ok bool) {
	kind, name, missingTerminator := parseSectionMarker(src.Text())
	// Only report one error per section marker, the most severe.
	switch {
	case name == "":
		p.addError(UnknownSectionMarker{src})
	case kind == "END":
		p.addError(UnstartedSectionError{src, name})
	case kind != "BEGIN":
		p.addError(UnknownSectionMarker{src})
	case missingTerminator:
		p.addError(UnterminatedSectionMarker{src})
	}
	return name, kind == "BEGIN" && name != ""
}

func (p *parser) checkSectionEnd(src Source, section *Section) {
	_, _, missingTerminator := parseSectionMarker(src.Text())
	if missingTerminator {
		p.addError(UnterminatedSectionMarker{src})
	}
}

func (p *parser) parseRogueSection(section *Section) {
	src := p.first()
	kind, name, missingTerminator := parseSectionMarker(src.Text())
	switch {
	case name == "":
		p.addError(UnknownSectionMarker{src})
	case kind == "START":
		p.addError(NestedSectionError{src, section})
	case kind != "END":
		p.addError(UnknownSectionMarker{src})
	case name != section.Name:
		p.addError(MismatchedSectionError{src, name, section})
	case missingTerminator:
		p.addError(UnterminatedSectionMarker{src})
	default:
		panic("unreachable")
	}
	p.parseInvalid()
}

func parseSectionMarker(marker string) (kind, name string, missingTerminator bool) {
	marker = strings.TrimPrefix(marker, sectionMarkerPrefix)
	marker, ok := strings.CutSuffix(marker, "===")
	if !ok {
		missingTerminator = true
	}

	markerType, name, ok := strings.Cut(marker, " ")
	if !ok {
		return "", "", missingTerminator
	}
	return markerType, name, missingTerminator
}

func (p *parser) parseSuffixBlock() {
	suffixes := &Suffixes{
		Source: p.takeWhileNot(isEmpty),
	}

	p.pushState(suffixes.Source)
	defer func() {
		suffixes.Blocks = p.popState()
		p.addBlock(suffixes)
	}()

	for p.parseWhitespace() {
		next := p.first().Text()
		switch {
		case isPotentialSectionMarker(next):
			p.parseInvalid()
		case isComment(next):
			p.parseComment()
		default:
			p.parseSuffix()
		}
	}
}

func (p *parser) parseSuffix() {
	s := p.first().Text()
	exception := false
	if strings.HasPrefix(s, "!") {
		exception = true
		s = s[1:]
		return
	}

	labels, wildcard, err := parseDNSLabels(s)
	if err != nil {
		p.addError(err)
		p.parseInvalid()
		return
	}

	if !exception {
		p.addBlock(&Suffix{
			Source:   p.takeOne(),
			Suffix:   labels,
			Wildcard: wildcard,
		})
		return
	}

	if wildcard {
		// XXXXXXXXX: an error
		p.parseInvalid()
		return
	}

	prev, ok := p.lastBlock().(*Suffix)
	if !ok {
		// XXXXXXXX error
		p.parseInvalid()
		return
	}

	if !labels.IsDirectChildOf(prev.Suffix) {
		// XXXXXXXX error
		p.parseInvalid()
		return
	}

	if slices.ContainsFunc(prev.Exceptions, func(e DNSLabels) bool { return slices.Equal(e, labels) }) {
		// XXXXXXXX errors
		p.parseInvalid()
		return
	}

	prev.Source.append(p.takeOne())
	prev.Exceptions = append(prev.Exceptions, labels)
}

func parseDNSLabels(s string) (labels DNSLabels, wildcard bool, err error) {
	labels = strings.Split(s, ".")
	// TODO: much better validation: leading/trailing dots, IDNA,
	// label lengths, empty labels, ...
	if labels[0] == "*" {
		return labels[1:], true, nil
	}
	return labels, false, nil
}

func isEmpty(s string) bool                  { return s == "" }
func isPotentialSectionMarker(s string) bool { return strings.HasPrefix(s, sectionMarkerPrefix) }
func isComment(s string) bool {
	if isPotentialSectionMarker(s) {
		return false
	}
	if isPotentialEndOfGroup(s) {
		return false
	}
	return strings.HasPrefix(s, commentPrefix)
}
func isStartOfGroup(s string) bool {
	// Just one group right now, the Amazon generated block
	return s == "// Amazon : https://www.amazon.com/"
}
func isPotentialEndOfGroup(s string) bool {
	return strings.HasPrefix(s, "// concludes ")
}

func (p *parser) parseWhitespace() bool {
	src := p.takeWhile(isEmpty)
	if !src.empty() {
		p.addBlock(&BlankLines{src})
	}
	return p.empty()
}

func (p *parser) parseComment() {
	src := p.takeWhile(isComment)
	if !src.empty() {
		p.addBlock(&Comment{src})
	}
}

func (p *parser) parseInvalid() {
	// Try to group invalid lines together, if the previous line was
	// invalid as well.
	if inv, ok := p.lastBlock().(*InvalidSource); ok {
		inv.append(p.takeOne())
		return
	}
	p.addBlock(&InvalidSource{p.takeOne()})
}

///////
// General parsing utility functions
///////

func (p *parser) pushState(src Source) {
	p.stack = append(p.stack, p.state)
	p.state = state{Source: src}
}

func (p *parser) popState() []Block {
	popped := p.state

	last := len(p.stack) - 1
	p.stack, p.state = p.stack[:last], p.stack[last]

	return popped.blocks
}

func (p *parser) addBlock(b Block) {
	p.state.blocks = append(p.state.blocks, b)
}

func (p *parser) addError(err error) {
	// TODO: put exemptions back
	p.errs = append(p.errs, err)
}

func (p *parser) lastBlock() Block {
	if len(p.state.blocks) == 0 {
		return nil
	}
	return p.state.blocks[len(p.state.blocks)-1]
}

// // addBlock adds b to p.File.Blocks.
// func (p *parser) addBlock(b Block) {
// 	p.File.Blocks = append(p.File.Blocks, b)
// }

// // addError records err as a parse/validation error.
// //
// // If err matches a legacy exemption from current validation rules,
// // err is recorded as a non-fatal warning instead.
// func (p *parser) addError(err error) {
// 	if p.downgradeToWarning(err) {
// 		p.File.Warnings = append(p.File.Warnings, err)
// 	} else {
// 		p.File.Errors = append(p.File.Errors, err)
// 	}
// }
