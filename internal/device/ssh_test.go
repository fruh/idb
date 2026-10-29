package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/danielpaulus/go-ios/ios"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestShellJoin(t *testing.T) {
	if got := shellJoin([]string{"echo", "it's", "a b"}); got != `'echo' 'it'\''s' 'a b'` {
		t.Fatalf("got %s", got)
	}
}

func TestKnownHostsTOFU(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "known_hosts")
	k1, k2 := testSigner(t).PublicKey(), testSigner(t).PublicKey()
	if err := checkKnownHost(path, "U1", k1); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := checkKnownHost(path, "U1", k1); err != nil {
		t.Fatalf("same key: %v", err)
	}
	if err := checkKnownHost(path, "U1", k2); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed key must be rejected, got %v", err)
	}
	if err := checkKnownHost(path, "U2", k2); err != nil {
		t.Fatalf("other device is independent: %v", err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
}

// Fake sshd: password auth, answers every exec with the command text on stdout and exit status 3.
func fakeSSHD(t *testing.T, c net.Conn) { fakeSSHDCount(t, c, nil) }

func fakeSSHDCount(t *testing.T, c net.Conn, attempts *atomic.Int32) {
	cfg := &ssh.ServerConfig{PasswordCallback: func(m ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if attempts != nil {
			attempts.Add(1)
		}
		if m.User() == "root" && string(pw) == "alpine" {
			return nil, nil
		}
		return nil, os.ErrPermission
	}}
	cfg.AddHostKey(testSigner(t))
	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, creqs, _ := nc.Accept()
		go func() {
			for r := range creqs {
				if r.Type != "exec" {
					r.Reply(false, nil)
					continue
				}
				r.Reply(true, nil)
				ch.Write([]byte("ran:" + string(r.Payload[4:])))
				ch.SendRequest("exit-status", false, []byte{0, 0, 0, 3})
				ch.Close()
			}
		}()
	}
}

// tcpPair returns connected loopback conns; net.Pipe deadlocks the SSH version exchange.
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ch := make(chan net.Conn, 1)
	go func() { c, _ := l.Accept(); ch <- c }()
	cli, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return cli, <-ch
}

func TestRunSSHCommandExitCode(t *testing.T) {
	cli, srv := tcpPair(t)
	go fakeSSHD(t, srv)
	cfg := SSHConfig{User: "root", Password: "alpine", KnownHostsPath: filepath.Join(t.TempDir(), "kh")}
	code, err := runSSH(cli, "UDID", cfg, []string{"uname", "-a"})
	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestRunSSHBadPassword(t *testing.T) {
	cli, srv := tcpPair(t)
	go fakeSSHD(t, srv)
	cfg := SSHConfig{User: "root", Password: "wrong", KnownHostsPath: filepath.Join(t.TempDir(), "kh")}
	if _, err := runSSH(cli, "UDID", cfg, []string{"id"}); err == nil {
		t.Fatal("expected auth failure")
	}
}

func TestSSHShellNoSSHSentinel(t *testing.T) {
	old := dialPortFn
	defer func() { dialPortFn = old }()

	dialPortFn = func(ios.DeviceEntry, uint16) (net.Conn, error) { return nil, errors.New("port refused") }
	if _, err := SSHShell(ios.DeviceEntry{}, SSHConfig{User: "mobile"}, nil); !errors.Is(err, ErrNoSSH) {
		t.Fatalf("closed port must be ErrNoSSH, got %v", err)
	}

	dialPortFn = func(ios.DeviceEntry, uint16) (net.Conn, error) { return nil, fmt.Errorf("%w: boom", errUsbmuxd) }
	if _, err := SSHShell(ios.DeviceEntry{}, SSHConfig{User: "mobile"}, nil); errors.Is(err, ErrNoSSH) {
		t.Fatalf("usbmuxd failure must not be ErrNoSSH: %v", err)
	}
}

func TestBadLoginIsNotNoSSH(t *testing.T) {
	cli, srv := tcpPair(t)
	go fakeSSHD(t, srv)
	cfg := SSHConfig{User: "mobile", Password: "x", KnownHostsPath: filepath.Join(t.TempDir(), "kh")}
	_, err := runSSH(cli, "UDID", cfg, nil)
	if err == nil || errors.Is(err, ErrNoSSH) {
		t.Fatalf("want plain auth error, got %v", err)
	}
}

func TestDefaultPassword(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("IDB_SSH_PASSWORD", "")
	run := func(cfg SSHConfig) (int, error, int32) {
		cli, srv := tcpPair(t)
		var n atomic.Int32
		go fakeSSHDCount(t, srv, &n)
		cfg.User = "root"
		cfg.KnownHostsPath = filepath.Join(t.TempDir(), "kh")
		code, err := runSSH(cli, "UDID", cfg, []string{"id"})
		return code, err, n.Load()
	}
	if _, err, n := run(SSHConfig{}); err != nil || n != 1 {
		t.Fatalf("default should log in with one attempt, err=%v attempts=%d", err, n)
	}
	if _, err, n := run(SSHConfig{NoDefaultPassword: true}); err == nil || n != 0 {
		t.Fatalf("opt-out must not try a password, err=%v attempts=%d", err, n)
	}
	if _, err, n := run(SSHConfig{Password: "wrong"}); err == nil || n != 1 {
		t.Fatalf("explicit wrong password must not also try the default, err=%v attempts=%d", err, n)
	}
}

// Fake sshd accepting only "s3cret"; counts attempts.
func promptRun(t *testing.T, cfg SSHConfig, explicit string, answers []string) (int32, error, int) {
	t.Helper()
	cli, srv := tcpPair(t)
	var n atomic.Int32
	go func() {
		c := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			n.Add(1)
			if string(pw) == "s3cret" {
				return nil, nil
			}
			return nil, os.ErrPermission
		}}
		c.AddHostKey(testSigner(t))
		sc, chans, reqs, err := ssh.NewServerConn(srv, c)
		if err != nil {
			return
		}
		go ssh.DiscardRequests(reqs)
		go func() {
			for nc := range chans {
				nc.Reject(ssh.Prohibited, "test")
			}
		}()
		_ = sc
	}()
	asked := 0
	prompt := func(string) (string, error) {
		if asked >= len(answers) {
			return "", errNoTerminal
		}
		asked++
		return answers[asked-1], nil
	}
	cc, chans, reqs, err := ssh.NewClientConn(cli, "x", &ssh.ClientConfig{
		User:            "mobile",
		Auth:            []ssh.AuthMethod{passwordAuth(cfg, explicit, prompt)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err == nil {
		ssh.NewClient(cc, chans, reqs).Close()
	}
	return n.Load(), err, asked
}

func TestPasswordFlow(t *testing.T) {
	if n, err, asked := promptRun(t, SSHConfig{}, "", []string{"s3cret"}); err != nil || n != 2 || asked != 1 {
		t.Fatalf("default rejected then prompt: err=%v attempts=%d asked=%d", err, n, asked)
	}
	if n, err, asked := promptRun(t, SSHConfig{NoDefaultPassword: true}, "", []string{"s3cret"}); err != nil || n != 1 || asked != 1 {
		t.Fatalf("opt-out prompts first: err=%v attempts=%d asked=%d", err, n, asked)
	}
	if n, err, asked := promptRun(t, SSHConfig{}, "wrong", []string{"s3cret"}); err != nil || n != 2 || asked != 1 {
		t.Fatalf("explicit wrong then retype, no alpine: err=%v attempts=%d asked=%d", err, n, asked)
	}
	if n, err, _ := promptRun(t, SSHConfig{}, "", nil); err == nil || n != 1 || !strings.Contains(err.Error(), "no terminal") {
		t.Fatalf("no terminal after default: err=%v attempts=%d", err, n)
	}
	if n, err, asked := promptRun(t, SSHConfig{}, "", []string{"a", "b", "c", "d"}); err == nil || n != 3 || asked != 2 {
		t.Fatalf("max 3 tries total: err=%v attempts=%d asked=%d", err, n, asked)
	}
}
