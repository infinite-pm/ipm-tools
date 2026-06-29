package cli

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

// PrintDefaults writes flag defaults using double-dash prefixes.
func PrintDefaults(fs *flag.FlagSet, w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fs.VisitAll(func(f *flag.Flag) {
		usage := f.Usage
		if f.DefValue != "" {
			usage = fmt.Sprintf("%s (default %s)", usage, f.DefValue)
		}
		fmt.Fprintf(tw, "  --%s\t%s\n", f.Name, usage)
	})
	_ = tw.Flush()
}
