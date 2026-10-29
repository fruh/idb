package mux

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestResolvePrecedence(t *testing.T) {
	resetFallback(t)
	dialFn = func(string, string, time.Duration) (net.Conn, error) { return nil, errors.New("no fallback") }
	t.Setenv(EnvHost, "envhost:1111")
	t.Setenv(envMuxd, "/var/run/other")
	if got := Resolve("flaghost:2222"); got != "tcp://flaghost:2222" {
		t.Fatalf("flag should win, got %q", got)
	}
	if got := Resolve(""); got != "tcp://envhost:1111" {
		t.Fatalf("IDB_HOST should beat USBMUXD_SOCKET_ADDRESS, got %q", got)
	}
	t.Setenv(EnvHost, "")
	if got := Resolve(""); got != "unix:///var/run/other" {
		t.Fatalf("got %q", got)
	}
	t.Setenv(envMuxd, "")
	if got := Resolve(""); got != "" {
		t.Fatalf("default should be empty, got %q", got)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"host.docker.internal":      "tcp://host.docker.internal:" + DefaultPort,
		"host.docker.internal:5555": "tcp://host.docker.internal:5555",
		"TCP://1.2.3.4:9":           "tcp://1.2.3.4:9",
		"/tmp/usbmuxd":              "unix:///tmp/usbmuxd",
		"unix:///var/run/usbmuxd":   "unix:///var/run/usbmuxd",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// A fake usbmuxd on a unix socket that upper-cases whatever it receives.
func TestRelayBothDirections(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "usbmuxd")
	up, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	go func() {
		c, err := up.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 5)
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		c.Write([]byte("pong:" + string(buf)))
	}()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, l, "unix://"+sock) }()

	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte("hello"))
	got := make([]byte, len("pong:hello"))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "pong:hello" {
		t.Fatalf("got %q, err %v", got, err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v", err)
	}
}

func resetFallback(t *testing.T) {
	t.Helper()
	fallbackOnce = sync.Once{}
	fallbackAddr = ""
	noFallback.Store(false)
	t.Setenv(EnvHost, "")
	t.Setenv(envMuxd, "")
	t.Setenv("IDB_NO_FALLBACK", "")
	oldStat, oldDial, oldTarget := statFn, dialFn, fallbackTarget
	t.Cleanup(func() {
		statFn, dialFn, fallbackTarget = oldStat, oldDial, oldTarget
		fallbackOnce = sync.Once{}
		fallbackAddr = ""
		noFallback.Store(false)
	})
	statFn = func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist }
}

func TestFallbackUsedWhenSocketMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	resetFallback(t)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	fallbackTarget = l.Addr().String()
	if got := Resolve(""); got != "tcp://"+fallbackTarget {
		t.Fatalf("got %q", got)
	}
	if !FellBack("") || Hint("") != "" {
		t.Fatal("expected fallback and no hint")
	}
}

func TestFallbackUnreachableGivesHint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	resetFallback(t)
	dialFn = func(string, string, time.Duration) (net.Conn, error) { return nil, errors.New("nope") }
	if got := Resolve(""); got != "" {
		t.Fatalf("got %q", got)
	}
	if Hint("") == "" {
		t.Fatal("expected hint")
	}
}

func TestNoProbeWhenSocketExistsOrExplicitOrDisabled(t *testing.T) {
	resetFallback(t)
	dialFn = func(string, string, time.Duration) (net.Conn, error) {
		t.Fatal("must not probe")
		return nil, nil
	}
	statFn = func(string) (os.FileInfo, error) { return nil, nil } // socket exists
	if Resolve("") != "" {
		t.Fatal("socket exists: want default")
	}
	resetFallback(t)
	dialFn = func(string, string, time.Duration) (net.Conn, error) {
		t.Fatal("must not probe")
		return nil, nil
	}
	if got := Resolve("flag:1"); got != "tcp://flag:1" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("IDB_NO_FALLBACK", "1")
	if Resolve("") != "" {
		t.Fatal("disabled: want default")
	}
	t.Setenv("IDB_NO_FALLBACK", "")
	DisableFallback()
	if Resolve("") != "" {
		t.Fatal("DisableFallback: want default")
	}
}
