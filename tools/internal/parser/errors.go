package parser

import (
	"fmt"
	"strings"
)

// InvalidEncodingError reports that the input is encoded with
// something other than UTF-8.
type InvalidEncodingError struct {
	Encoding string
}

func (e InvalidEncodingError) Error() string {
	return fmt.Sprintf("file uses invalid character encoding %s", e.Encoding)
}

// UTF8BOMError reports that the input has an unnecessary UTF-8 byte
// order mark (BOM) at the start.
type UTF8BOMError struct{}

func (e UTF8BOMError) Error() string {
	return "file starts with an unnecessary UTF-8 BOM (byte order mark)"
}

// InvalidUTF8Error reports that a line contains bytes that are not
// valid UTF-8.
type InvalidUTF8Error struct {
	Line Source
}

func (e InvalidUTF8Error) Error() string {
	return fmt.Sprintf("found non UTF-8 bytes at %s", e.Line.LocationString())
}

// DOSNewlineError reports that a line has a DOS style line ending.
type DOSNewlineError struct {
	Line Source
}

func (e DOSNewlineError) Error() string {
	return fmt.Sprintf("%s has a DOS line ending (\\r\\n instead of just \\n)", e.Line.LocationString())
}

// TrailingWhitespaceError reports that a line has trailing whitespace.
type TrailingWhitespaceError struct {
	Line Source
}

func (e TrailingWhitespaceError) Error() string {
	return fmt.Sprintf("%s has trailing whitespace", e.Line.LocationString())
}

// LeadingWhitespaceError reports that a line has leading whitespace.
type LeadingWhitespaceError struct {
	Line Source
}

func (e LeadingWhitespaceError) Error() string {
	return fmt.Sprintf("%s has leading whitespace", e.Line.LocationString())
}

// SectionInSuffixBlock reports that a comment within a block of
// suffixes contains a section delimiter.
type SectionInSuffixBlock struct {
	Line Source
}

func (e SectionInSuffixBlock) Error() string {
	return fmt.Sprintf("section delimiters are not allowed in suffix block comment at %s", e.Line.LocationString())
}

// UnclosedSectionError reports that a file section was not closed
// properly before EOF.
type UnclosedSectionError struct {
	*Section
}

func (e UnclosedSectionError) Error() string {
	return fmt.Sprintf("section %q at %s, but is never closed", e.Name, e.LocationString())
}

// NestedSectionError reports that a file section is being started
// while already within a section, which the PSL format does not
// allow.
type NestedSectionError struct {
	Source

	Name    string
	Section *Section
}

func (e NestedSectionError) Error() string {
	return fmt.Sprintf("new section %q started at %s while still in section %q (started at %s)", e.Name, e.LocationString(), e.Section.Name, e.Section.LocationString())
}

// UnstartedSectionError reports that a file section end marker was
// found without a corresponding start.
type UnstartedSectionError struct {
	Source

	Name string
}

func (e UnstartedSectionError) Error() string {
	return fmt.Sprintf("section %q closed at %s but was not started", e.Name, e.LocationString())
}

// MismatchedSectionError reports that a file section was started
// under one name but ended under another.
type MismatchedSectionError struct {
	Source

	EndName string
	Section *Section
}

func (e MismatchedSectionError) Error() string {
	return fmt.Sprintf("section %q closed at %s while in section %q (started at %s)", e.EndName, e.LocationString(), e.Section.Name, e.Section.LocationString())
}

// UnknownSectionMarker reports that a line looks like a file section
// marker (e.g. "===BEGIN ICANN DOMAINS==="), but is not one of the
// recognized kinds of marker.
type UnknownSectionMarker struct {
	Line Source
}

func (e UnknownSectionMarker) Error() string {
	return fmt.Sprintf("unknown kind of section marker %q at %s", e.Line.Text(), e.Line.LocationString())
}

// UnterminatedSectionMarker reports that a section marker is missing
// the required trailing "===", e.g. "===BEGIN ICANN DOMAINS".
type UnterminatedSectionMarker struct {
	Source
}

func (e UnterminatedSectionMarker) Error() string {
	return fmt.Sprintf(`section marker %q at %s is missing trailing "==="`, e.Text(), e.LocationString())
}

// MissingEntityName reports that a block of suffixes does not have a
// parseable owner name in its header comment.
type MissingEntityName struct {
	Suffixes Suffixes
}

func (e MissingEntityName) Error() string {
	return fmt.Sprintf("could not find entity name for %s at %s", e.Suffixes.shortName(), e.Suffixes.LocationString())
}

// MissingEntityEmail reports that a block of suffixes does not have a
// parseable contact email address in its header comment.
type MissingEntityEmail struct {
	Suffixes Suffixes
}

func (e MissingEntityEmail) Error() string {
	return fmt.Sprintf("could not find a contact email for %s at %s", e.Suffixes.shortName(), e.Suffixes.LocationString())
}

// SuffixBlocksInWrongPlace reports that some suffix blocks of the
// private section are in the wrong sort order.
type SuffixBlocksInWrongPlace struct {
	// EditScript is a list of suffix block movements to put the
	// private domains section in the correct order. Note that each
	// step assumes that the previous steps have already been done.
	EditScript []MoveSuffixBlock
}

// MoveSuffixBlock describes the movement of one suffix block to a
// different place in the PSL file.
type MoveSuffixBlock struct {
	// Name is the name of the block to be moved.
	Name string
	// InsertAfter is the name of the block that is immediately before
	// the correct place to insert Block, or the empty string if Block
	// should go first in the private domains section.
	InsertAfter string
}

func (e SuffixBlocksInWrongPlace) Error() string {
	if len(e.EditScript) == 1 {
		after := e.EditScript[0].InsertAfter
		if after == "" {
			return fmt.Sprintf("suffix block %q is in the wrong place, should be at the start of the private section", e.EditScript[0].Name)
		} else {
			return fmt.Sprintf("suffix block %q is in the wrong place, it should go immediately after block %q", e.EditScript[0].Name, e.EditScript[0].InsertAfter)
		}
	}

	var ret strings.Builder
	fmt.Fprintf(&ret, "%d suffix blocks are in the wrong place, make these changes to fix:\n", len(e.EditScript))

	for _, edit := range e.EditScript {
		fmt.Fprintf(&ret, "\tmove block: %s\n", edit.Name)
		if edit.InsertAfter == "" {
			fmt.Fprintf(&ret, "\t        to: start of private section\n")
		} else {
			fmt.Fprintf(&ret, "\t     after: %s\n", edit.InsertAfter)
		}
	}

	return ret.String()
}

type ExceptionAndWildcardSuffixError struct {
	Source
}

func (e ExceptionAndWildcardSuffixError) Error() string {
	return fmt.Sprintf("suffix %q at %s is both a wildcard exception and a wildcard, which is not allowed", e.Text(), e.LocationString())
}

type ExceptionNotDirectlyFollowingBaseError struct {
	Source
}

func (e ExceptionNotDirectlyFollowingBaseError) Error() string {
	return fmt.Sprintf("exception %q at %s must directly follow the suffix it's modifying", e.Text(), e.LocationString())
}

type InvalidExceptionError struct {
	Source
	Parent *Suffix
}

func (e InvalidExceptionError) Error() string {
	return fmt.Sprintf("exception %q at %s is not a valid exception to the wildcard %q", e.Text(), e.LocationString(), e.Parent.Text())
}

type DuplicateExceptionError struct {
	Source
}

func (e DuplicateExceptionError) Error() string {
	return fmt.Sprintf("duplicate exception %q at %s", e.Text(), e.LocationString())
}

// MismatchedGroupError reports that a file group was started
// under one name but ended under another.
type MismatchedGroupError struct {
	Source

	EndName string
	Group   *Group
}

func (e MismatchedGroupError) Error() string {
	return fmt.Sprintf("group %q closed at %s while in group %q (started at %s)", e.EndName, e.LocationString(), e.Group.Name, e.Group.LocationString())
}

// UnclosedGroupError reports that a file group was not closed
// properly before EOF.
type UnclosedGroupError struct {
	*Group
}

func (e UnclosedGroupError) Error() string {
	return fmt.Sprintf("group %q at %s is never closed", e.Name, e.LocationString())
}

// NestedGroupError reports that a file group is being started
// while already within a group, which the PSL format does not
// allow.
type NestedGroupError struct {
	Source

	Name  string
	Group *Group
}

func (e NestedGroupError) Error() string {
	return fmt.Sprintf("new group %q started at %s while still in group %q (started at %s)", e.Name, e.LocationString(), e.Group.Name, e.Group.LocationString())
}
