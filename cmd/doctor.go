package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/device"
	"github.com/fruh/idb/internal/mux"
	"github.com/fruh/idb/internal/version"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check usbmuxd connectivity and device state",
		RunE: func(*cobra.Command, []string) error {
			fmt.Println(version.String())
			addr := mux.Resolve(flagHost)
			switch {
			case addr == "":
				addr = mux.Local() + " (local default)"
			case mux.FellBack(flagHost):
				addr += " (auto-fallback: local usbmuxd socket missing)"
			}
			fmt.Println("usbmuxd endpoint:", addr)
			devs, err := device.List(true)
			if err != nil {
				fmt.Println("[FAIL] cannot talk to usbmuxd:", err)
				fmt.Println("  host:      is usbmuxd / Apple Devices / Finder running? On Linux install usbmuxd.")
				if h := mux.Hint(flagHost); h != "" {
					fmt.Println("  container:", h)
				} else {
					fmt.Println("  container: run `idb server` on the host and set IDB_HOST=host.docker.internal:" + mux.DefaultPort)
				}
				return err
			}
			fmt.Println("[ OK ] usbmuxd reachable")
			if len(devs) == 0 {
				fmt.Println("[WARN] no devices attached")
				return nil
			}
			for _, d := range devs {
				if d.Name == "" {
					fmt.Printf("[WARN] %s: attached but lockdown failed (unlock the device and tap Trust)\n", d.UDID)
					continue
				}
				fmt.Printf("[ OK ] %s  %s  %s  iOS %s (%s)\n", d.UDID, d.Name, d.Model, d.ProductVer, d.Connection)
			}
			return nil
		},
	}
}
