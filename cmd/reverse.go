package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/fruh/idb/internal/device"
	"github.com/spf13/cobra"
)

// parseReverse validates the device port and expands a bare target port to localhost:port.
func parseReverse(devPort, target string) (uint16, string, error) {
	dp, err := strconv.ParseUint(devPort, 10, 16)
	if err != nil || dp == 0 {
		return 0, "", fmt.Errorf("device port must be a number 1-65535, got %q", devPort)
	}
	if n, err := strconv.ParseUint(target, 10, 16); err == nil && n > 0 {
		return uint16(dp), net.JoinHostPort("localhost", target), nil
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" {
		return 0, "", fmt.Errorf("target must be a port or host:port, got %q", target)
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return 0, "", fmt.Errorf("invalid target port %q", port)
	}
	return uint16(dp), target, nil
}

func reverseCmd() *cobra.Command {
	var cfg device.SSHConfig
	var insecure bool
	var bind string
	c := &cobra.Command{
		Use:   "reverse <device-port> <target>",
		Short: "Expose a host service on the device (like adb reverse; jailbroken devices, until Ctrl-C)",
		Long: `Makes OpenSSH on the device listen on <device-port> and sends every connection back
to <target> (a port, meaning localhost:port, or host:port), dialled from where idb runs.
From a container, use host.docker.internal:<port> as the target to reach the host.
Needs a jailbreak with sshd (same login options as "idb shell") and runs until Ctrl-C
or until the SSH session ends. Stock iOS has no way to do this over USB.`,
		Example: "  idb reverse 8080 8080\n  idb reverse 8080 host.docker.internal:8080",
		Args:    cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			dp, target, err := parseReverse(args[0], args[1])
			if err != nil {
				return err
			}
			d, err := selected()
			if err != nil {
				return err
			}
			cfg.InsecureHostKey = insecure
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			fmt.Fprintf(os.Stderr, "reverse device:%s:%d -> %s (Ctrl-C to stop)\n", bind, dp, target)
			return device.Reverse(ctx, d, cfg, bind, dp, target)
		},
	}
	addSSHFlags(c.Flags(), &cfg, &insecure)
	c.Flags().StringVar(&bind, "bind", "127.0.0.1", "address sshd listens on, on the device")
	return c
}
