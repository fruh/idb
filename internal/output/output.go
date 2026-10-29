// Package output prints tables and JSON.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// JSON pretty-prints v.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table prints rows with aligned columns.
func Table(w io.Writer, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for i, h := range header {
		if i > 0 {
			fmt.Fprint(tw, "\t")
		}
		fmt.Fprint(tw, h)
	}
	fmt.Fprintln(tw)
	for _, r := range rows {
		for i, c := range r {
			if i > 0 {
				fmt.Fprint(tw, "\t")
			}
			fmt.Fprint(tw, c)
		}
		fmt.Fprintln(tw)
	}
	tw.Flush()
}
