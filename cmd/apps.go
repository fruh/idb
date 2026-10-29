package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/device"
	"github.com/fruh/idb/internal/output"
)

func lsAppsCmd() *cobra.Command {
	var user, system, asJSON, paths bool
	c := &cobra.Command{
		Use:     "ls-apps",
		Aliases: []string{"apps", "list"},
		Short:   "List installed apps with bundle and data-container paths",
		RunE: func(*cobra.Command, []string) error {
			kind := "all"
			switch {
			case user && system:
				return fmt.Errorf("--user and --system are mutually exclusive")
			case user:
				kind = "user"
			case system:
				kind = "system"
			}
			d, err := selected()
			if err != nil {
				return err
			}
			apps, err := device.Apps(d, kind)
			if err != nil {
				return err
			}
			if asJSON {
				return output.JSON(os.Stdout, apps)
			}
			if paths {
				for _, a := range apps {
					fmt.Printf("%s\t%s\n", a.BundleID, a.Path)
				}
				return nil
			}
			rows := make([][]string, 0, len(apps))
			for _, a := range apps {
				rows = append(rows, []string{a.BundleID, a.Name, a.Version, a.Type, a.Path, a.Container})
			}
			output.Table(os.Stdout, []string{"BUNDLE ID", "NAME", "VERSION", "TYPE", "PATH", "CONTAINER"}, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&user, "user", false, "only user-installed apps")
	c.Flags().BoolVar(&system, "system", false, "only system apps")
	c.Flags().BoolVar(&paths, "paths", false, "print only `bundle-id<TAB>path` (like `adb shell pm list packages -f`)")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func installCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <app.ipa>",
		Short: "Install a signed .ipa (or .app directory)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if _, err := os.Stat(args[0]); err != nil {
				return err
			}
			d, err := selected()
			if err != nil {
				return err
			}
			if err := device.Install(d, args[0]); err != nil {
				return fmt.Errorf("install failed (the IPA must be signed with a profile that includes this device): %w", err)
			}
			fmt.Println("Success")
			return nil
		},
	}
}

func uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <bundle-id>",
		Short: "Uninstall an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := selected()
			if err != nil {
				return err
			}
			if err := device.Uninstall(d, args[0]); err != nil {
				return err
			}
			fmt.Println("Success")
			return nil
		},
	}
}
