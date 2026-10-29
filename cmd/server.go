package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fruh/idb/internal/daemon"
	"github.com/fruh/idb/internal/device"
	"github.com/fruh/idb/internal/mux"
)

// serverCfg is the parsed configuration of `idb server`.
type serverCfg struct {
	Bind, Port, Upstream, PairDir string
	Tunnel                        bool
	TunnelPort                    int
}

// args rebuilds the flags for the detached child from parsed values.
func (c serverCfg) args() []string {
	a := []string{"server", "--foreground", "--bind", c.Bind, "--port", c.Port, "--upstream", c.Upstream,
		"--tunnel-port", strconv.Itoa(c.TunnelPort)}
	if c.Tunnel {
		a = append(a, "--tunnel", "--pair-records", c.PairDir)
	}
	return a
}

// settingsDiffer reports whether explicitly passed flags disagree with the running server.
func settingsDiffer(st daemon.State, want serverCfg, changed func(string) bool) bool {
	return (changed("bind") && want.Bind != st.Bind) ||
		(changed("port") && want.Port != st.Port) ||
		(changed("upstream") && want.Upstream != st.Upstream) ||
		(changed("tunnel") && want.Tunnel != st.Tunnel) ||
		(changed("tunnel-port") && st.Tunnel && want.TunnelPort != st.TunnelPort)
}

func serverCmd() *cobra.Command {
	var cfg serverCfg
	var foreground bool
	c := &cobra.Command{
		Use:     "server",
		Aliases: []string{"start-server"},
		Short:   "Start the host relay daemon (usbmuxd over TCP) or print its status",
		Long: `Starts a background server that relays the host's usbmuxd to a TCP port, so containers
can reach your devices (they find it at host.docker.internal:<port> automatically, or via
IDB_HOST). If it is already running, only its status is printed. Stop it with "idb kill-server".

--foreground keeps the server attached to the terminal (use it as a Docker, launchd or
systemd entrypoint). State and log live in ~/.idb/server.json and ~/.idb/server.log.

Docker Desktop (macOS/Windows) reaches the host's loopback, so the default
--bind 127.0.0.1 works. On Linux use the docker bridge address, e.g.
--bind 172.17.0.1, with --add-host host.docker.internal:host-gateway.
The relay is unauthenticated: anyone who can reach it can use your paired devices.`,
		RunE: func(c *cobra.Command, _ []string) error {
			if cfg.Upstream == "" {
				cfg.Upstream = mux.Local()
			}
			if foreground {
				return runServer(cfg)
			}
			return startDaemon(c, cfg)
		},
	}
	f := c.Flags()
	f.StringVar(&cfg.Bind, "bind", "127.0.0.1", "address to listen on")
	f.StringVar(&cfg.Port, "port", mux.DefaultPort, "TCP port to listen on")
	f.BoolVar(&cfg.Tunnel, "tunnel", false, "also run the iOS 17+ tunnel agent (userspace, no root) for ps/launch/kill/screenshot")
	f.StringVar(&cfg.PairDir, "pair-records", filepath.Join(device.HomeDir(), "pair"), "directory for the tunnel agent's own pairing identity")
	f.StringVar(&cfg.Upstream, "upstream", "", "usbmuxd address (default: local socket)")
	f.BoolVar(&foreground, "foreground", false, "stay attached to the terminal instead of daemonizing")
	return c
}

// fill adds the persistent --tunnel-port, which is not a flag of this command.
func (c *serverCfg) fill() { c.TunnelPort = flagTunnelPort }

func startDaemon(c *cobra.Command, cfg serverCfg) error {
	cfg.fill()
	if err := os.MkdirAll(device.HomeDir(), 0o700); err != nil {
		return err
	}
	unlock, err := device.LockDir(device.HomeDir())
	if err != nil {
		return err
	}
	defer unlock()

	if st, err := daemon.Load(); err == nil {
		if st.Running() {
			fmt.Println("idb server running:", st.Status())
			if settingsDiffer(st, cfg, c.Flags().Changed) {
				fmt.Println("note: running with different settings; run `idb kill-server` first to apply")
			}
			return nil
		}
		daemon.Remove(0) // stale
	}

	pid, done, err := daemon.Spawn(cfg.args())
	if err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return fmt.Errorf("server exited during startup:\n%s", daemon.LogTail(10))
		case <-time.After(100 * time.Millisecond):
		}
		if st, err := daemon.Load(); err == nil && st.PID == pid && st.Running() {
			fmt.Println("idb server started:", st.Status())
			return nil
		}
	}
	return fmt.Errorf("server (pid %d) did not become ready in 5s; see %s", pid, daemon.LogPath())
}

func runServer(cfg serverCfg) error {
	cfg.fill()
	if st, err := daemon.Load(); err == nil && st.Running() {
		return fmt.Errorf("idb server already running (pid %d)", st.PID)
	}
	addr := net.JoinHostPort(cfg.Bind, cfg.Port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(cfg.Bind); ip == nil || !ip.IsLoopback() {
		fmt.Fprintf(os.Stderr, "warning: listening on %s without authentication\n", addr)
	}
	fmt.Fprintf(os.Stderr, "idb server: %s -> %s\n", addr, cfg.Upstream)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Tunnel {
		warnLegacyPair(cfg.PairDir)
		if cfg.Upstream != mux.Local() {
			return fmt.Errorf("--tunnel must run on the host that owns usbmuxd, not with a custom --upstream")
		}
		relay := ""
		if ip := net.ParseIP(cfg.Bind); ip != nil && !ip.IsLoopback() {
			if ip.IsUnspecified() {
				return fmt.Errorf("--tunnel with --bind %s is ambiguous; bind a specific address such as the docker bridge IP", cfg.Bind)
			}
			relay = cfg.Bind
		}
		fmt.Fprintf(os.Stderr, "idb server: tunnel agent on %s:%d (userspace)\n", cfg.Bind, cfg.TunnelPort)
		go func() {
			if err := device.RunTunnelAgent(ctx, cfg.PairDir, cfg.Bind, cfg.TunnelPort, relay); err != nil {
				fmt.Fprintln(os.Stderr, "tunnel agent:", err)
				stop()
			}
		}()
	}
	st := daemon.State{PID: os.Getpid(), Bind: cfg.Bind, Port: cfg.Port, Upstream: cfg.Upstream,
		Tunnel: cfg.Tunnel, TunnelPort: cfg.TunnelPort, PairRecords: cfg.PairDir, Started: time.Now()}
	if err := st.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: cannot write state file:", err)
	}
	defer daemon.Remove(os.Getpid())
	return mux.Serve(ctx, l, cfg.Upstream)
}

func killServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill-server",
		Short: "Stop the idb server daemon",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			st, err := daemon.Load()
			if err != nil {
				fmt.Println("idb server is not running")
				return nil
			}
			if !st.Running() {
				daemon.Remove(0)
				fmt.Println("idb server is not running (removed stale state)")
				return nil
			}
			if err := daemon.Stop(st, 5*time.Second); err != nil {
				return err
			}
			fmt.Printf("idb server stopped (pid %d)\n", st.PID)
			return nil
		},
	}
}

// warnLegacyPair points at the pre-~/.idb location of the tunnel identity
// instead of silently copying key material.
func warnLegacyPair(dir string) {
	if _, err := os.Stat(dir); err == nil {
		return
	}
	if st, err := os.Stat(".idb-pair"); err == nil && st.IsDir() {
		fmt.Fprintf(os.Stderr, "idb: found ./.idb-pair from an older version; to keep that tunnel identity run: mv .idb-pair %s\n", dir)
	}
}
