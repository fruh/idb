package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/device"
)

// logWriter picks the destination: the console, a file (append), or both.
func logWriter(path string, tee bool, console io.Writer) (io.Writer, func() error, error) {
	if path == "" {
		if tee {
			return nil, nil, errors.New("--tee needs --output")
		}
		return console, func() error { return nil }, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	if tee {
		return io.MultiWriter(f, console), f.Close, nil
	}
	return f, f.Close, nil
}

func logcatCmd() *cobra.Command {
	var process, grep, output string
	var tee bool
	c := &cobra.Command{
		Use:     "logcat",
		Aliases: []string{"logs", "syslog"},
		Short:   "Stream the device syslog",
		Example: "  idb logcat -p SpringBoard\n  idb logcat -o ios.log          # append to a file only\n  idb logcat -o ios.log --tee    # file and console",
		RunE: func(*cobra.Command, []string) error {
			out, closeOut, err := logWriter(output, tee, os.Stdout)
			if err != nil {
				return err
			}
			defer closeOut()
			d, err := selected()
			if err != nil {
				return err
			}
			var keep func(string) bool
			if process != "" || grep != "" {
				keep = func(l string) bool {
					return (process == "" || strings.Contains(l, " "+process+"[")) &&
						(grep == "" || strings.Contains(strings.ToLower(l), strings.ToLower(grep)))
				}
			}
			if output != "" {
				fmt.Fprintf(os.Stderr, "logging to %s (append)\n", output)
			}
			return device.Syslog(d, out, keep)
		},
	}
	c.Flags().StringVarP(&process, "process", "p", "", "only lines from this process name")
	c.Flags().StringVarP(&grep, "grep", "g", "", "only lines containing this text (case-insensitive)")
	c.Flags().StringVarP(&output, "output", "o", "", "append the log to this file instead of the console")
	c.Flags().BoolVarP(&tee, "tee", "t", false, "with --output, also print to the console")
	return c
}
