package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/device"
	"github.com/fruh/idb/internal/mux"
	"github.com/fruh/idb/internal/output"
)

var (
	flagTunnelPort  int
	flagNoAutoMount bool
)

// developer selects a device for developer services, using the tunnel agent
// next to the resolved usbmuxd on iOS 17+.
func developer() (ios.DeviceEntry, error) {
	if !flagNoAutoMount {
		d, err := selected()
		if err != nil {
			return d, err
		}
		notify := func(format string, a ...any) { fmt.Fprintf(os.Stderr, "idb: "+format+"\n", a...) }
		if err := device.EnsureImage(d, device.ImageCacheDir(), notify); err != nil {
			return d, err
		}
	}
	return device.SelectDeveloper(flagSerial, device.TunnelCfg{
		Host: device.TunnelHost(mux.Resolve(flagHost)),
		Port: flagTunnelPort,
	})
}

func psCmd() *cobra.Command {
	var asJSON, apps bool
	c := &cobra.Command{
		Use:   "ps",
		Short: "List running processes (developer services)",
		RunE: func(*cobra.Command, []string) error {
			d, err := developer()
			if err != nil {
				return err
			}
			procs, err := device.Processes(d)
			if err != nil {
				return err
			}
			if apps {
				kept := procs[:0]
				for _, p := range procs {
					if p.Application {
						kept = append(kept, p)
					}
				}
				procs = kept
			}
			if asJSON {
				return output.JSON(os.Stdout, procs)
			}
			rows := make([][]string, 0, len(procs))
			for _, p := range procs {
				rows = append(rows, []string{strconv.FormatUint(p.PID, 10), p.Name})
			}
			output.Table(os.Stdout, []string{"PID", "NAME"}, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&apps, "apps", false, "only applications")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func launchCmd() *cobra.Command {
	var envs []string
	var restart bool
	c := &cobra.Command{
		Use:   "launch <bundle-id> [args...]",
		Short: "Launch an app (developer services)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			env := map[string]string{}
			for _, e := range envs {
				k, v, ok := strings.Cut(e, "=")
				if !ok {
					return fmt.Errorf("--env wants KEY=VALUE, got %q", e)
				}
				env[k] = v
			}
			d, err := developer()
			if err != nil {
				return err
			}
			pid, err := device.Launch(d, args[0], args[1:], env, restart)
			if err != nil {
				return err
			}
			fmt.Println(pid)
			return nil
		},
	}
	c.Flags().StringArrayVarP(&envs, "env", "e", nil, "environment variable KEY=VALUE (repeatable)")
	c.Flags().BoolVar(&restart, "restart", false, "kill a running instance first")
	return c
}

func killCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill <pid|process-name>",
		Short: "Terminate a process (developer services)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := developer()
			if err != nil {
				return err
			}
			if pid, err := strconv.ParseUint(args[0], 10, 64); err == nil {
				return device.Kill(d, pid)
			}
			_, err = device.KillByName(d, args[0])
			return err
		},
	}
}

func mountCmd() *cobra.Command {
	var cache string
	c := &cobra.Command{
		Use:   "mount",
		Short: "Download and mount the developer disk image if none is mounted",
		Long: `Developer services (ps, launch, kill, screenshot) need a mounted developer image.
idb does this automatically before those commands (--no-auto-mount disables it).
The image is cached in ~/.idb/images and downloaded on the machine running this command, so it also works from a container.
iOS 17+ additionally requires Developer Mode enabled on the device.`,
		RunE: func(*cobra.Command, []string) error {
			d, err := selected()
			if err != nil {
				return err
			}
			if ok, err := device.ImageMounted(d); err == nil && ok {
				fmt.Println("developer image already mounted")
				return nil
			}
			if err := device.MountImage(d, cache); err != nil {
				return err
			}
			fmt.Println("mounted")
			return nil
		},
	}
	c.Flags().StringVar(&cache, "cache", device.ImageCacheDir(), "directory to cache downloaded images (env IDB_IMAGES)")
	return c
}
