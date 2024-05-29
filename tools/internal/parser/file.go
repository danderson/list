package parser

// File is a parsed PSL file.
// A PSL file consists of blocks separated by an empty line. Most
// blocks are annotated lists of suffixes, but some are plain
// top-level comments or delimiters for sections of the file.
type File struct {
	// Blocks are the data blocks of the file, in the order they
	// appear.
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

// AllSuffixBlocks returns all suffix blocks in f.
func (f *File) AllSuffixBlocks() []Suffixes {
	var ret []Suffixes

	for _, block := range f.Blocks {
		switch v := block.(type) {
		case Suffixes:
			ret = append(ret, v)
		}
	}

	return ret
}

// SuffixBlocksInSection returns all suffix blocks within the named
// file section (for example, "ICANN DOMAINS" or "PRIVATE DOMAINS").
func (f *File) SuffixBlocksInSection(name string) []Suffixes {
	var ret []Suffixes

	var curSection string
	for _, block := range f.Blocks {
		switch v := block.(type) {
		case StartSection:
			curSection = v.Name
		case EndSection:
			if curSection == name {
				return ret
			}
			curSection = ""
		case Suffixes:
			if curSection == name {
				ret = append(ret, v)
			}
		}
	}
	return ret
}
