// Package cmd defines the idb command line.
package cmd

import (
	"os"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/device"
	"github.com/fruh/idb/internal/mux"
	"github.com/fruh/idb/internal/version"
)

var (
	flagSerial  string
	flagHost    string
	flagVerbose bool
)

// Root builds the root command.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "idb",
		Version:       version.String(),
		Short:         "adb-style tool for iOS devices (host or Docker)",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(c *cobra.Command, _ []string) {
			if c.Name() == "server" || c.Name() == "kill-server" {
				mux.DisableFallback()
			}
			device.SetLogLevel(flagVerbose)
			mux.Apply(flagHost)
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.PersistentFlags().StringVarP(&flagSerial, "serial", "s", os.Getenv("IDB_SERIAL"), "device UDID (env IDB_SERIAL)")
	root.PersistentFlags().StringVarP(&flagHost, "host", "H", "", "usbmuxd/idb server address, e.g. host.docker.internal:27016 (env IDB_HOST)")
	root.PersistentFlags().IntVar(&flagTunnelPort, "tunnel-port", ios.HttpApiPort(), "tunnel agent port for iOS 17+ developer services (env GO_IOS_AGENT_PORT)")
	root.PersistentFlags().BoolVar(&flagNoAutoMount, "no-auto-mount", os.Getenv("IDB_NO_AUTO_MOUNT") != "", "do not download/mount the developer image automatically (env IDB_NO_AUTO_MOUNT)")
	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose go-ios logging")
	root.AddCommand(
		devicesCmd(), infoCmd(), lsAppsCmd(), logcatCmd(),
		installCmd(), uninstallCmd(), serverCmd(), killServerCmd(), doctorCmd(),
		lsCmd(), pullCmd(), pushCmd(), rmCmd(), shellCmd(), forwardCmd(), crashlogsCmd(), screenshotCmd(),
		psCmd(), launchCmd(), killCmd(), mountCmd(), reverseCmd(), versionCmd(),
	)
	return root
}

func selected() (ios.DeviceEntry, error) {
	return device.Select(flagSerial)
}

// ErrorHint returns extra guidance to print after a failed command, or "".
func ErrorHint() string { return mux.Hint(flagHost) }
