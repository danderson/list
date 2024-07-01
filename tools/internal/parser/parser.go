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

// Even though the PSL format looks like a flat file, this parser
// produces a tree because we turn some of the file's implicit
// structure into explicit nodes.
//
// Using Python's type annotation syntax, this parser produces the
// following tree:
//
//       toplevel : list[section | comment | whitespace | invalid]
//        section : list[group | suffix_block | whitespace | invalid]
//          group : list[suffix_block | whitespace | invalid]
//   suffix_block : list[comment | suffix]
//        comment : <some unparsed comment lines>
//         suffix : <one suffix and its exceptions>
//     whitespace : <one or more blank lines>
//        invalid : <some amount of text that failed to parse>
//
// The "whitespace" and "invalid" nodes may seem weird to keep, but
// they are essential for machine editing and rewriting, so that we
// can unparse back into raw bytes with a precisely chosen layout,
// even with an invalid PSL file.
//
// In terms of code, the parser does basic recursive descent: for each
// left-hand side symbol above, there is a function that consumes some
// input lines, potentially calls other parsers to generate child
// nodes, and finally returns one node for that symbol to its caller.

// parser is the state of an in-progress parse.
type parser struct {
	// Source is the source text being parsed. As the parsing
	// advances, it gets mutated in place to contain only remaining
	// unparsed input.
	Source

	// blocks are the AST nodes that have been produced so far.
	blocks []Block

	// errs are the errors that have been produced so far.
	errs []error
}

// addBlock adds b to the parser's output.
func (p *parser) addBlock(b Block) {
	p.blocks = append(p.blocks, b)
}

// addError adds err to the parser's output.
func (p *parser) addError(err error) {
	// TODO: put exemptions back
	p.errs = append(p.errs, err)
}

// The main parsing logic needs a few stateless helpers to classify
// lines of input, and to extract structural information from within
// the lines. We define them all upfront here, along with the magic
// constants they need, so that the parser doesn't have to keep
// redefining them.

const (
	commentPrefix       = "// "
	sectionMarkerPrefix = "// ==="
	sectionStart        = "// ===BEGIN "
	sectionEnd          = "// ===END "

	// Right now, the only group in the PSL is the bulk-managed Amazon
	// suffixes, so we hardcode group parsing for that.
	//
	// The parsing logic and generated AST are still generic and
	// support many groups in theory, so if we agree on a syntax for
	// others to use, very little code is aware that "groups" actually
	// just mean a single hardcoded magical group.
	amazonGroupStart = "// Amazon : https://www.amazon.com/"
	amazonGroupEnd   = "// concludes Amazon"
)

func isEmpty(s string) bool {
	return s == ""
}

func isSectionStart(s string) bool {
	return strings.HasPrefix(s, sectionStart)
}

func isSectionEnd(s string) bool {
	return strings.HasSuffix(s, sectionEnd)
}

func isAnySectionMarker(s string) bool {
	return strings.HasPrefix(s, sectionMarkerPrefix)
}

func isGroupStart(s string) bool {
	return s == amazonGroupStart
}

func isGroupEnd(s string) bool {
	return s == amazonGroupEnd
}

func isComment(s string) bool {
	return (strings.HasPrefix(s, commentPrefix) &&
		!isAnySectionMarker(s) &&
		!isGroupStart(s) &&
		!isGroupEnd(s))
}

// parseSectionMarker breaks up a section marker string of the form
// "// ===<VERB> <NAME>===" and returns its component parts.
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

// parseGroupStartMarker breaks up a start-of-group marker and returns
// its component parts.
//
// Currently only one hardcoded group exists, and its start marker is
// the exact string given in amazonGroupStart.
func parseGroupStartMarker(marker string) (groupName string, ok bool) {
	if marker != amazonGroupStart {
		return "", false
	}
	return "Amazon", true
}

// parseGroupEndMarker breaks up an end-of-group marker and returns its component parts.
//
// Currently only one hardcoded group exists, and its end marker is
// the exact string given in amazonGroupEnd.
func parseGroupEndMarker(marker string) (groupName string, ok bool) {
	if marker != amazonGroupEnd {
		return "", false
	}
	return "Amazon", true
}

// parseDNSLabels parses a domain name into its component labels.
func parseDNSLabels(s string) (labels DNSLabels, err error) {
	labels = strings.Split(s, ".")

	// TODO: full DNS validation needs to go here: leading/trailing
	// dots, IDNA, label lengths, empty labels, ...

	return labels, nil
}

// The main recursive descent parser begins here. The functions are
// named for one of the symbols in the grammar described earlier, and
// all consume prefixes of the parser.Source.
//
// For example, a parser function can consume 0 lines of source, or
// the first 14 lines, or the entire remaining input, but they do not
// selectively consume lines [1-2, 6-8] while leaving lines 3-5 still
// in the input.

// parseTopLevel consumes all the input and returns the top-level
// File.
func (p *parser) parseTopLevel() *File {
	for p.parseWhitespace() {
		next := p.first().Text()
		switch {
		case isComment(next):
			p.parseComment()
		case isPotentialSectionMarker(next):
			p.parseSection()
		default:
			p.parseInvalid()
		}
	}
	return &File{
		Blocks: p.blocks,
		Errors: p.errs,
		// XXXXXX TODO: warnings
	}
}

// parseWhitespace parses blank lines (if any) into a BlankLines block
// and reports whether there is any input left to process.
func (p *parser) parseWhitespace() bool {
	src := p.takeWhile(isEmpty)
	if !src.empty() {
		p.addBlock(&BlankLines{src})
	}
	return p.empty()
}

// parseComment consumes comment lines into a Comment block.
func (p *parser) parseComment() {
	src := p.takeWhile(isComment)
	if !src.empty() {
		panic("unexpected: parseComment called but no comment found")
	}
	p.addBlock(&Comment{src})
}

// parseInvalid consumes one line into an InvalidSource block.
func (p *parser) parseInvalid() {
	p.addBlock(&InvalidSource{p.takeOne()})
}

// parseSection parses the start of p.Source as a PSL section
// (e.g. ===BEGIN ICANN DOMAINS=== .. ===END ICANN DOMAINS===).
func (p *parser) parseSection() {
	start := p.takeOne()
	kind, name, missingTerminator := parseSectionMarker(start.Text())
	// Only report one error per section marker, the most severe.
	ok := false
	switch {
	case name == "" || (kind != "BEGIN" && kind != "END"):
		p.addError(UnknownSectionMarker{src})
	case kind == "END":
		p.addError(UnstartedSectionError{src, name})
	case missingTerminator:
		p.addError(UnterminatedSectionMarker{src})
	default:
		ok = true
	}
	if !ok {
		p.addBlock(InvalidSource{start})
		return
	}

	section := &Section{
		Source: start,
		Name:   name,
	}

	defs := p.takeWhileNot(isPotentialSectionMarker)
	section.Source.append(defs)

	sub := parser{Source: defs}
	for sub.parseWhitespace() {
		next := sub.first().Text()
		switch {
		case isPotentialSectionMarker(next):
			sub.parseRogueSection(section)
		case isStartOfGroup(next):
			sub.parseGroup(section)
		default:
			sub.parseSuffixBlock()
		}
	}
}

func (p *parser) checkSectionEnd(src Source, section *Section) (consumeMarker bool) {
	kind, name, missingTerminator := parseSectionMarker(src.Text())
	switch {
	case name == "":
		p.addError(UnknownSectionMarker{src})
		if missingTerminator {
			p.addError(UnterminatedSectionMarker{src})
		}
	}
}

func (p *parser) parseRogueSection(section *Section) {
	src := p.first()
	kind, name, missingTerminator := parseSectionMarker(src.Text())
	switch {
	case name == "" || (kind != "BEGIN" && kind != "END"):
		p.addError(UnknownSectionMarker{src})
	case kind == "BEGIN":
		p.addError(NestedSectionError{src, name, section})
	case name != section.Name:
		p.addError(MismatchedSectionError{src, name, section})
	case missingTerminator:
		p.addError(UnterminatedSectionMarker{src})
	default:
		panic("unreachable")
	}
	p.parseInvalid()
}

func (p *parser) parseGroup(section *Section) {
	src, hasEnd := p.takeUntil(isPotentialEndOfGroup)
	name, ok := parseGroupStart(src.first().Text())
	if !ok {
		panic("parseGroup called but wasn't start of a group")
	}

	group := &Group{
		Source: src,
		Name:   name,
	}
	p.addBlock(group)
	p.pushState(group.Source)
	defer func() {
		group.Blocks = p.popState()
	}()

	if hasEnd {
		if endName := parseGroupEndMarker(src.last().Text()); endName != group.Name {
			p.addError(MismatchedGroupError{src, endName, group})
		}
	} else {
		p.addError(UnclosedGroupError{group})
	}

	for p.parseWhitespace() {
		next := p.first().Text()
		switch {
		case isPotentialSectionMarker(next):
			p.parseRogueSection(section)
		case isStartOfGroup(next):
			p.parseRogueGroup(group)
		default:
			p.parseSuffixBlock()
		}
	}
}

func (p *parser) parseRogueGroup(group *Group) {
	src := p.first()
	name, ok := parseGroupStart(src.Text())
	if !ok {
		// Panic because right now there is one hardcoded group, how
		// can we possibly fail to parse it?
		panic("unparseable rogue group")
	}
	p.addError(NestedGroupError{src, name, group})
	p.parseInvalid()
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
	src := p.first()

	if strings.HasPrefix(src.Text(), "!") {
		p.parseSuffixException()
		return
	}

	labels, wildcard, err := parseDNSLabels(src.Text())
	if err != nil {
		p.addError(err)
		p.parseInvalid()
		return
	}

	p.addBlock(&Suffix{
		Source:   p.takeOne(),
		Suffix:   labels,
		Wildcard: wildcard,
	})
}

func (p *parser) parseSuffixException() {
	src := p.first()
	labels, wildcard, err := parseDNSLabels(src.Text()[1:])
	if err != nil {
		p.addError(err)
		p.parseInvalid()
		return
	}

	if wildcard {
		p.addError(ExceptionAndWildcardSuffixError{src})
		p.parseInvalid()
		return
	}

	prev, ok := p.lastBlock().(*Suffix)
	if !ok {
		p.addError(ExceptionNotDirectlyFollowingBaseError{src})
		p.parseInvalid()
		return
	}

	if !labels.IsDirectChildOf(prev.Suffix) {
		p.addError(InvalidExceptionError{src, prev})
		p.parseInvalid()
		return
	}

	if slices.ContainsFunc(prev.Exceptions, func(e DNSLabels) bool { return slices.Equal(e, labels) }) {
		p.addError(DuplicateExceptionError{src})
		p.parseInvalid()
		return
	}

	prev.Source.append(p.takeOne())
	prev.Exceptions = append(prev.Exceptions, labels)
}

///////
// General parsing utility functions
///////

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
