package cmd

import (
	"fmt"

	"github.com/fruh/idb/internal/version"
	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	var short bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Print the idb version",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if short {
				fmt.Println(version.Short())
			} else {
				fmt.Println(version.String())
			}
			return nil
		},
	}
	c.Flags().BoolVar(&short, "short", false, "print only the version number")
	return c
}
