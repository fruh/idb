package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/device"
)

func logcatCmd() *cobra.Command {
	var process, grep string
	c := &cobra.Command{
		Use:     "logcat",
		Aliases: []string{"logs", "syslog"},
		Short:   "Stream the device syslog",
		RunE: func(*cobra.Command, []string) error {
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
			return device.Syslog(d, os.Stdout, keep)
		},
	}
	c.Flags().StringVarP(&process, "process", "p", "", "only lines from this process name")
	c.Flags().StringVarP(&grep, "grep", "g", "", "only lines containing this text (case-insensitive)")
	return c
}
