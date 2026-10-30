package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/fruh/idb/internal/device"
)

var flagApp string

func addAppFlag(c *cobra.Command) {
	c.Flags().StringVarP(&flagApp, "app", "a", "", "bundle id: operate inside this app's container instead of the media partition")
}

func openFS() (*device.FS, error) {
	d, err := selected()
	if err != nil {
		return nil, err
	}
	return device.OpenFS(d, flagApp)
}

func lsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ls [path]",
		Short: "List remote files (media partition, or an app container with -a)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			fs, err := openFS()
			if err != nil {
				return err
			}
			defer fs.Close()
			p := "/"
			if len(args) == 1 {
				p = device.Join("/", args[0])
			}
			return printDir(fs, p)
		},
	}
	addAppFlag(c)
	return c
}

func printDir(fs *device.FS, p string) error {
	entries, err := fs.ReadDir(p)
	if err != nil {
		return err
	}
	for _, e := range entries {
		kind, suffix := "-", ""
		if e.Info.IsDir() {
			kind, suffix = "d", "/"
		} else if e.Info.IsLink() {
			kind, suffix = "l", " -> "+e.Info.LinkTarget
		}
		fmt.Printf("%s %10d  %s%s\n", kind, e.Info.Size, e.Name, suffix)
	}
	return nil
}

func pullCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pull <remote> [local]",
		Short: "Copy a remote file or directory to the host",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			fs, err := openFS()
			if err != nil {
				return err
			}
			defer fs.Close()
			remote := device.Join("/", args[0])
			local := path.Base(remote)
			if len(args) == 2 {
				local = args[1]
			}
			if st, err := os.Stat(local); err == nil && st.IsDir() {
				local = filepath.Join(local, path.Base(remote))
			}
			return fs.Pull(remote, local)
		},
	}
	addAppFlag(c)
	return c
}

func pushCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "push <local> <remote>",
		Short: "Copy a host file to the device",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			fs, err := openFS()
			if err != nil {
				return err
			}
			defer fs.Close()
			return fs.Push(args[0], device.Join("/", args[1]))
		},
	}
	addAppFlag(c)
	return c
}

func rmCmd() *cobra.Command {
	var recursive bool
	c := &cobra.Command{
		Use:   "rm <remote>",
		Short: "Delete a remote file (or directory with -r)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			fs, err := openFS()
			if err != nil {
				return err
			}
			defer fs.Close()
			p := device.Join("/", args[0])
			if p == "/" {
				return fmt.Errorf("refusing to remove /")
			}
			if recursive {
				return fs.RemoveAll(p)
			}
			return fs.Remove(p)
		},
	}
	c.Flags().BoolVarP(&recursive, "recursive", "r", false, "remove directories recursively")
	addAppFlag(c)
	return c
}

// shellCmd opens a real SSH shell on jailbroken devices and falls back to a
// file browser (AFC / house_arrest) when sshd is not reachable.
func shellCmd() *cobra.Command {
	var forceSSH, files, insecure bool
	var cfg device.SSHConfig
	c := &cobra.Command{
		Use:   "shell [flags] [-- command args...]",
		Short: "Shell on the device: SSH (jailbroken), else a file browser",
		Long: `Opens a full PTY shell through OpenSSH on the device (port 22, over usbmuxd, so it
also works from a container), or runs the command after "--". Default user is "mobile".
Auth order: -i key, ssh-agent, --password / IDB_SSH_PASSWORD, the jailbreak default "alpine"
(only when no password was given; --no-default-password disables it), then a prompt.
Host keys are pinned per UDID in ~/.idb/known_hosts.

If sshd is not reachable (stock iOS has no shell) and no command was given, idb falls
back to a small file browser over the media partition: ls, cd, pwd, cat, pull, push,
rm, mkdir, help, exit. A failed login is an error, not a fallback.

--files skips SSH and opens the file browser; -a <bundle-id> browses that app's container.
--ssh insists on SSH and never falls back.
For installed apps and their bundle paths use "idb ls-apps".`,
		RunE: func(_ *cobra.Command, args []string) error {
			if files || flagApp != "" {
				if len(args) > 0 {
					return fmt.Errorf("running a command needs SSH; drop --files/-a")
				}
				return browse()
			}
			d, err := selected()
			if err != nil {
				return err
			}
			cfg.InsecureHostKey = insecure
			code, err := device.SSHShell(d, cfg, args)
			if err != nil {
				if shouldFallback(err, forceSSH, args) {
					fmt.Fprintf(os.Stderr, "idb: %v\nidb: falling back to the file browser\n", err)
					return browse()
				}
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&forceSSH, "ssh", false, "require SSH, never fall back to the file browser")
	f.BoolVar(&files, "files", false, "skip SSH and open the file browser")
	addSSHFlags(f, &cfg, &insecure)
	addAppFlag(c)
	return c
}

// shouldFallback: only "no sshd" with an interactive request, and only when SSH was not forced.
func shouldFallback(err error, forceSSH bool, cmd []string) bool {
	return errors.Is(err, device.ErrNoSSH) && !forceSSH && len(cmd) == 0
}

func browse() error {
	fs, err := openFS()
	if err != nil {
		return err
	}
	defer fs.Close()
	return repl(fs)
}

func repl(fs *device.FS) error {
	cwd := "/"
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%s:%s $ ", fs.Label, cwd)
		if !in.Scan() {
			fmt.Println()
			return nil
		}
		f := strings.Fields(in.Text())
		if len(f) == 0 {
			continue
		}
		arg := func(i int) string {
			if i < len(f) {
				return f[i]
			}
			return ""
		}
		var err error
		switch f[0] {
		case "exit", "quit":
			return nil
		case "help":
			fmt.Println("ls [path] | cd <path> | pwd | cat <file> | pull <remote> [local] | push <local> <remote> | rm [-r] <path> | mkdir <path> | exit")
		case "pwd":
			fmt.Println(cwd)
		case "ls":
			err = printDir(fs, device.Join(cwd, arg(1)))
		case "cd":
			p := device.Join(cwd, arg(1))
			var st interface{ IsDir() bool }
			if i, e := fs.Stat(p); e != nil {
				err = e
			} else {
				st = i
			}
			if err == nil && !st.IsDir() {
				err = fmt.Errorf("%s: not a directory", p)
			} else if err == nil {
				cwd = p
			}
		case "cat":
			err = fs.Cat(device.Join(cwd, arg(1)), os.Stdout)
		case "pull":
			if arg(1) == "" {
				err = fmt.Errorf("usage: pull <remote> [local]")
				break
			}
			remote := device.Join(cwd, arg(1))
			local := arg(2)
			if local == "" {
				local = path.Base(remote)
			}
			err = fs.Pull(remote, local)
		case "push":
			if arg(1) == "" || arg(2) == "" {
				err = fmt.Errorf("usage: push <local> <remote>")
				break
			}
			err = fs.Push(arg(1), device.Join(cwd, arg(2)))
		case "rm":
			if arg(1) == "-r" {
				err = fs.RemoveAll(device.Join(cwd, arg(2)))
			} else if arg(1) != "" {
				err = fs.Remove(device.Join(cwd, arg(1)))
			}
		case "mkdir":
			err = fs.MkDir(device.Join(cwd, arg(1)))
		default:
			err = fmt.Errorf("%s: not available (iOS has no shell; type help)", f[0])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}

func forwardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forward <host-port> <device-port>",
		Short: "Forward a host TCP port to a port on the device (until Ctrl-C)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			hp, err1 := strconv.ParseUint(args[0], 10, 16)
			dp, err2 := strconv.ParseUint(args[1], 10, 16)
			if err1 != nil || err2 != nil {
				return fmt.Errorf("ports must be numbers 1-65535")
			}
			d, err := selected()
			if err != nil {
				return err
			}
			stop, err := device.Forward(d, uint16(hp), uint16(dp))
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "forwarding :%d -> device:%d (Ctrl-C to stop)\n", hp, dp)
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			return stop()
		},
	}
}

func crashlogsCmd() *cobra.Command {
	var out, pattern string
	var list bool
	c := &cobra.Command{
		Use:   "crashlogs",
		Short: "List or download crash reports",
		RunE: func(*cobra.Command, []string) error {
			d, err := selected()
			if err != nil {
				return err
			}
			if list {
				names, err := device.ListCrashlogs(d, pattern)
				if err != nil {
					return err
				}
				for _, n := range names {
					fmt.Println(n)
				}
				return nil
			}
			return device.Crashlogs(d, pattern, out)
		},
	}
	c.Flags().StringVarP(&out, "out", "o", "crashlogs", "download directory")
	c.Flags().StringVar(&pattern, "pattern", "*", "file name glob")
	c.Flags().BoolVar(&list, "list", false, "only list names")
	return c
}

func screenshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "screenshot [file.png]",
		Short: "Capture the screen (needs developer services)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := developer()
			if err != nil {
				return err
			}
			img, err := device.Screenshot(d)
			if err != nil {
				return err
			}
			name := fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
			if len(args) == 1 {
				name = args[0]
			}
			if err := os.WriteFile(name, img, 0o644); err != nil {
				return err
			}
			fmt.Println(name)
			return nil
		},
	}
}

// addSSHFlags registers the login options shared by shell and reverse.
func addSSHFlags(f *pflag.FlagSet, cfg *device.SSHConfig, insecure *bool) {
	f.StringVar(&cfg.User, "user", "mobile", "ssh user")
	f.StringVar(&cfg.Password, "password", "", "ssh password (env IDB_SSH_PASSWORD)")
	f.StringVarP(&cfg.IdentityFile, "identity", "i", "", "ssh private key file")
	f.Uint16Var(&cfg.Port, "ssh-port", 22, "sshd port on the device")
	f.BoolVar(&cfg.NoDefaultPassword, "no-default-password", false, "do not try the jailbreak default password \"alpine\"")
	f.BoolVar(insecure, "insecure-hostkey", false, "skip host key pinning")
}
