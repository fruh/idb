package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/device"
	"github.com/fruh/idb/internal/output"
)

func devicesCmd() *cobra.Command {
	var long, asJSON bool
	c := &cobra.Command{
		Use:   "devices",
		Short: "List attached devices",
		RunE: func(*cobra.Command, []string) error {
			devs, err := device.List(long || asJSON)
			if err != nil {
				return err
			}
			if asJSON {
				return output.JSON(os.Stdout, devs)
			}
			header := []string{"UDID", "CONNECTION"}
			if long {
				header = append(header, "NAME", "MODEL", "IOS")
			}
			rows := make([][]string, 0, len(devs))
			for _, d := range devs {
				r := []string{d.UDID, d.Connection}
				if long {
					r = append(r, d.Name, d.Model, d.ProductVer)
				}
				rows = append(rows, r)
			}
			output.Table(os.Stdout, header, rows)
			return nil
		},
	}
	c.Flags().BoolVarP(&long, "long", "l", false, "include name, model and iOS version")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Print all lockdown values of the device as JSON",
		RunE: func(*cobra.Command, []string) error {
			d, err := selected()
			if err != nil {
				return err
			}
			v, err := device.Values(d)
			if err != nil {
				return err
			}
			return output.JSON(os.Stdout, v)
		},
	}
}
