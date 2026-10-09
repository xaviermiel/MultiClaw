package analyze

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// section is a numbered part of the report.
type section struct {
	title  string
	fields []field
}

var flatten = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

// render writes the report: a header, then each section with its fields
// aligned.
func render(w io.Writer, an *analysis) error {
	bar := strings.Repeat("═", 70)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\n  TRANSACTION ANALYSIS %s\n%s\n", bar, an.hash.Hex(), bar)
	for i, s := range an.sections {
		fmt.Fprintf(tw, "\n%d. %s\n", i+1, s.title)
		for _, f := range s.fields {
			f.value = flatten.Replace(f.value) // values may come from the chain
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
