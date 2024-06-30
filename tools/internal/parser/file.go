package parser

import (
	"fmt"
	"net/mail"
	"net/url"
	"slices"
	"strings"

	"github.com/creachadair/mds/slice"
)

// File is a parsed PSL file.
// A PSL file consists of blocks separated by an empty line. Most
// blocks are annotated lists of suffixes, but some are plain
// top-level comments or delimiters for sections of the file.
type File struct {
	// Blocks are the top-level data blocks of the file, in the order
	// they appear. At this level, the only permitted block types are
	// Comment and Section.
	Blocks []Block
	// Errors are parse errors encountered while reading the
	// file. This includes fatal validation errors, not just malformed
	// syntax.
	Errors []error
	// Warnings are errors that were downgraded to just
	// warnings. Warnings are a concession to old PSL entries that now
	// have validation errors, due to PSL policy changes. As long as
	// the entries in question don't change, their preexisting
	// validation errors are downgraded to lint warnings.
	Warnings []error
}

// A Block is a parsed chunk of a PSL file.
// In Parse's output, a Block is one of the following concrete types:
// Comment, StartSection, EndSection, Suffixes.
type Block interface {
	Children() []Block
	source() Source
}

type BlankLines struct {
	Source
}

func (b *BlankLines) Children() []Block { return nil }
func (b *BlankLines) source() Source    { return b.Source }

type InvalidSource struct {
	Source
}

func (i *InvalidSource) Children() []Block { return nil }
func (i *InvalidSource) source() Source    { return i.Source }

// Comment is a standalone top-level comment block.
type Comment struct {
	Source
}

func (c Comment) Children() []Block { return nil }
func (c Comment) source() Source    { return c.Source }

type Section struct {
	Source

	Name   string // "ICANN DOMAINS"
	Blocks []Block
}

func (s *Section) Children() []Block { return s.Blocks }
func (s *Section) source() Source    { return s.Source }

type Group struct {
	Source

	Name   string // "Amazon"
	Blocks []Block
}

func (g *Group) Children() []Block { return g.Blocks }
func (g *Group) source() Source    { return g.Source }

// Suffixes is a list of PSL domain suffixes with optional additional
// metadata.
//
// Suffix sections consist of a header comment that contains a mix of
// structured and unstructured information, followed by a list of
// domain suffixes. The suffix list may contain additional
// unstructured inline comments.
type Suffixes struct {
	Source

	// Entity is the name of the entity responsible for this block of
	// suffixes.
	//
	// For ICANN suffixes, this is typically the TLD name or the NIC
	// that controls the TLD.
	//
	// For private domains this is the name of the legal entity (most
	// commonly a company) that owns all domains in the block.
	//
	// In a well-formed PSL file, Entity is non-empty for all suffix
	// blocks.
	Entity string
	// URL is a link to further information about the suffix block and
	// its managing entity.
	//
	// For ICANN domains this is typically the NIC's information page
	// for the TLD, or failing that a general information page such as
	// a Wikipedia entry.
	//
	// For private domains this is usually the responsible company's
	// website.
	//
	// May be nil when the block header doesn't have a URL.
	URLs []*url.URL
	// Emails is the contact name and email address of the person or
	// people responsible for this block of suffixes.
	//
	// This field may be nil if the block header doesn't have email
	// contact information.
	Emails []*mail.Address

	// Children is the contents of the suffix block, in the order it
	// appears in the file. Each element is a Suffix or a Comment.
	Blocks []Block
}

func (s Suffixes) source() Source    { return s.Source }
func (s Suffixes) Children() []Block { return s.Blocks }

// shortName returns either the quoted name of the responsible Entity,
// or a generic descriptor of this suffix block if Entity is unset.
func (s Suffixes) shortName() string {
	if s.Entity != "" {
		return fmt.Sprintf("%q", s.Entity)
	}
	return fmt.Sprintf("%d unowned suffixes", len(s.Entries))
}

type Suffix struct {
	Source

	Suffix     DNSLabels
	Wildcard   bool
	Exceptions []DNSLabels
}

func (s Suffix) source() Source    { return s.Source }
func (s Suffix) Children() []Block { return nil }

type DNSLabels []string

func (l DNSLabels) String() string {
	return strings.Join([]string(l), ".")
}

func (l DNSLabels) IsDirectChildOf(parent DNSLabels) bool {
	if len(l) != len(parent)+1 {
		return false
	}
	return slices.Equal(slice.Tail(l, len(parent)), parent)
}
