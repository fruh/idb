package device

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielpaulus/go-ios/ios"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

// SSHConfig describes how to log in to sshd on a jailbroken device.
type SSHConfig struct {
	User              string
	Password          string
	IdentityFile      string
	Port              uint16
	InsecureHostKey   bool
	KnownHostsPath    string // default <HomeDir>/known_hosts
	NoDefaultPassword bool
}

// ErrNoSSH means nothing is listening for SSH on the device (as opposed to a
// failed login), so callers may fall back to something else.
var ErrNoSSH = errors.New("sshd not reachable on the device")

// dialPortFn is replaced in tests.
var dialPortFn = DialPort

// errUsbmuxd marks failures to reach usbmuxd itself, which must not be mistaken for a closed sshd port.
var errUsbmuxd = errors.New("cannot reach usbmuxd")

// DialPort opens a TCP connection to a port on the device through usbmuxd, so
// it works the same on the host and from a container.
func DialPort(d ios.DeviceEntry, port uint16) (net.Conn, error) {
	m, err := ios.NewUsbMuxConnectionSimple()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUsbmuxd, err)
	}
	if err := m.Connect(d.DeviceID, port); err != nil {
		m.Close()
		return nil, fmt.Errorf("device port %d refused the connection: %w", port, err)
	}
	return m.ReleaseDeviceConnection().Conn(), nil
}

// SSHShell logs in and runs cmd, or an interactive shell when cmd is empty.
// It returns the remote exit status.
func SSHShell(d ios.DeviceEntry, cfg SSHConfig, cmd []string) (int, error) {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	conn, err := dialPortFn(d, cfg.Port)
	if err != nil {
		if !errors.Is(err, errUsbmuxd) {
			return 1, fmt.Errorf("%w: %v (stock iOS has no shell; a jailbreak with OpenSSH is required)", ErrNoSSH, err)
		}
		return 1, err
	}
	return runSSH(conn, d.Properties.SerialNumber, cfg, cmd)
}

// runSSH speaks SSH over an established connection to the device's sshd.
func runSSH(conn net.Conn, udid string, cfg SSHConfig, cmd []string) (int, error) {
	auth, closeAgent := authMethods(cfg)
	defer closeAgent()
	sc, chans, reqs, err := ssh.NewClientConn(conn, "idb-"+udid, &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(udid, cfg),
	})
	if err != nil {
		conn.Close()
		return 1, err
	}
	client := ssh.NewClient(sc, chans, reqs)
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return 1, err
	}
	defer sess.Close()
	sess.Stdout, sess.Stderr = os.Stdout, os.Stderr

	if len(cmd) > 0 {
		sess.Stdin = os.Stdin
		return exitStatus(sess.Run(shellJoin(cmd)))
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return 1, errors.New("interactive shell needs a terminal; pass a command after -- instead")
	}
	w, h, _ := term.GetSize(fd)
	if err := sess.RequestPty("xterm-256color", h, w, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return 1, err
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return 1, err
	}
	defer term.Restore(fd, old)
	stop := watchResize(fd, sess)
	defer stop()
	sess.Stdin = os.Stdin
	if err := sess.Shell(); err != nil {
		return 1, err
	}
	return exitStatus(sess.Wait())
}

func exitStatus(err error) (int, error) {
	var ee *ssh.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &ee):
		return ee.ExitStatus(), nil
	default:
		return 1, err
	}
}

// shellJoin quotes args for the remote login shell.
func shellJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

// defaultPassword is the well-known jailbreak default, tried once as a last
// resort when the user supplied no password at all.
const defaultPassword = "alpine"

// authMethods orders: identity file, ssh-agent, explicit password, the default
// password (only if no explicit one), interactive prompt.
func authMethods(cfg SSHConfig) ([]ssh.AuthMethod, func()) {
	var methods []ssh.AuthMethod
	closer := func() {}
	if cfg.IdentityFile != "" {
		if pem, err := os.ReadFile(cfg.IdentityFile); err == nil {
			if signer, err := ssh.ParsePrivateKey(pem); err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			} else {
				fmt.Fprintf(os.Stderr, "idb: ignoring identity %s: %v\n", cfg.IdentityFile, err)
			}
		}
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if c, err := net.Dial("unix", sock); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(c).Signers))
			closer = func() { c.Close() }
		}
	}
	pw := cfg.Password
	if pw == "" {
		pw = os.Getenv("IDB_SSH_PASSWORD")
	}
	methods = append(methods, passwordAuth(cfg, pw, ttyPrompt(cfg.User)))
	return methods, closer
}

// hostKeyCallback is trust-on-first-use, keyed by device UDID because the
// address is always the usbmuxd tunnel rather than a stable host.
func hostKeyCallback(udid string, cfg SSHConfig) ssh.HostKeyCallback {
	if cfg.InsecureHostKey {
		return ssh.InsecureIgnoreHostKey()
	}
	path := cfg.KnownHostsPath
	if path == "" {
		path = filepath.Join(HomeDir(), "known_hosts")
	}
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		return checkKnownHost(path, udid, key)
	}
}

func checkKnownHost(path, udid string, key ssh.PublicKey) error {
	line := udid + " " + key.Type() + " " + b64(key.Marshal())
	if data, err := os.ReadFile(path); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) < 3 || f[0] != udid || f[1] != key.Type() {
				continue
			}
			if f[2] == b64(key.Marshal()) {
				return nil
			}
			return fmt.Errorf("host key for device %s changed (possible tampering or a re-jailbreak); remove its line from %s or use --insecure-hostkey", udid, path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(os.Stderr, "idb: trusting new %s host key for %s (%s)\n", key.Type(), udid, ssh.FingerprintSHA256(key))
	_, err = fmt.Fprintln(f, line)
	return err
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

const maxPasswordTries = 3

var errNoTerminal = errors.New("password rejected and no terminal to ask for another; pass --password or -i")

// ttyPrompt asks for a password on the terminal.
func ttyPrompt(user string) func(string) (string, error) {
	return func(note string) (string, error) {
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			return "", errNoTerminal
		}
		if note != "" {
			fmt.Fprintln(os.Stderr, "idb:", note)
		}
		fmt.Fprintf(os.Stderr, "%s's password: ", user)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
}

// passwordAuth is a single "password" method (x/crypto runs each method name
// only once, so the default and the prompt must share one). Attempt 1 is the
// explicit password, else the default unless disabled; later attempts, and
// attempt 1 when there is nothing to try, go to the prompt.
func passwordAuth(cfg SSHConfig, explicit string, prompt func(note string) (string, error)) ssh.AuthMethod {
	attempt := 0
	return ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
		attempt++
		if attempt == 1 {
			if explicit != "" {
				return explicit, nil
			}
			if !cfg.NoDefaultPassword {
				return defaultPassword, nil
			}
		}
		note := ""
		if attempt == 2 && explicit == "" && !cfg.NoDefaultPassword {
			note = "default password rejected"
		}
		return prompt(note)
	}), maxPasswordTries)
}
