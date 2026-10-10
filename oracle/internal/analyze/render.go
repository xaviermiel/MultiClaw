package analyze

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"unicode"
)

// section is a numbered part of the report.
type section struct {
	title  string
	fields []field
}

// printable makes text that may come from the chain (token symbols, revert
// reasons, node errors) safe to print on one line: tabs and line breaks
// become spaces, other control characters (terminal escapes), text
// direction overrides and invalid UTF-8 become U+FFFD.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r):
			return unicode.ReplacementChar
		}
		return r
	}, s)
}

// render writes the report: a header, then each section with its fields
// aligned.
func render(w io.Writer, an *analysis) error {
	bar := strings.Repeat("═", 70)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\n  TRANSACTION ANALYSIS %s\n%s\n", bar, an.hash.Hex(), bar)
	for i, s := range an.sections {
		fmt.Fprintf(tw, "\n%d. %s\n", i+1, s.title)
		for _, f := range s.fields {
			f.key, f.value = printable(f.key), printable(f.value)
			if f.key == "" {
				// No tab: does not take part in the alignment.
				fmt.Fprintf(tw, "  %s\n", f.value)
			} else {
				fmt.Fprintf(tw, "  %s:\t%s\n", f.key, f.value)
			}
		}
	}
	fmt.Fprintf(tw, "\n%s\n", bar)
	return tw.Flush()
}
