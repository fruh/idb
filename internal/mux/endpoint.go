// Package mux resolves where usbmuxd lives (local socket or a remote relay)
// and implements the TCP relay behind `idb server`.
package mux

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultPort is the TCP port of `idb server`. It is deliberately not 27015:
	// on Windows the Apple usbmuxd service already listens there.
	DefaultPort = "27016"

	EnvHost = "IDB_HOST"
	// envMuxd is the variable go-ios reads to locate usbmuxd.
	envMuxd = "USBMUXD_SOCKET_ADDRESS"
)

var (
	// Hooks for tests.
	statFn = os.Stat
	dialFn = net.DialTimeout

	fallbackTarget = "host.docker.internal:" + DefaultPort

	fallbackOnce sync.Once
	fallbackAddr string
	noFallback   atomic.Bool
)

// DisableFallback turns the host.docker.internal probe off (used by `idb server`,
// which must talk to the local usbmuxd).
func DisableFallback() { noFallback.Store(true) }

// Explicit returns the endpoint the user asked for, or "". Precedence: flag,
// IDB_HOST, USBMUXD_SOCKET_ADDRESS.
func Explicit(flagHost string) string {
	for _, v := range []string{flagHost, os.Getenv(EnvHost), os.Getenv(envMuxd)} {
		if v != "" {
			return normalize(v)
		}
	}
	return ""
}

// Resolve returns the usbmuxd address in go-ios format (scheme://addr), or ""
// when the platform default should be used. Beyond Explicit it auto-falls back
// to the host's `idb server` at host.docker.internal when the local usbmuxd
// socket does not exist (the container case); the probe runs at most once.
func Resolve(flagHost string) string {
	if v := Explicit(flagHost); v != "" {
		return v
	}
	return autoFallback()
}

// FellBack reports whether Resolve would use the auto-fallback endpoint.
func FellBack(flagHost string) bool {
	// Apply exports the fallback via USBMUXD_SOCKET_ADDRESS, so compare values
	// instead of testing for "nothing explicit".
	fb := autoFallback()
	return fb != "" && Resolve(flagHost) == fb
}

func localSocketMissing() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	_, err := statFn(strings.TrimPrefix(Local(), "unix://"))
	return errors.Is(err, fs.ErrNotExist)
}

func autoFallback() string {
	if noFallback.Load() || os.Getenv("IDB_NO_FALLBACK") != "" {
		return ""
	}
	fallbackOnce.Do(func() {
		if !localSocketMissing() {
			return
		}
		c, err := dialFn("tcp", fallbackTarget, time.Second)
		if err != nil {
			return
		}
		c.Close()
		fallbackAddr = "tcp://" + fallbackTarget
	})
	return fallbackAddr
}

// Hint explains how to get a usable endpoint when nothing explicit is set, the
// local socket is missing and the fallback did not answer; "" otherwise.
func Hint(flagHost string) string {
	if Explicit(flagHost) != "" || autoFallback() != "" || noFallback.Load() || !localSocketMissing() {
		return ""
	}
	return "no usbmuxd at " + strings.TrimPrefix(Local(), "unix://") + " and no idb server at " + fallbackTarget +
		": run `idb server` on the host (Linux: --bind 172.17.0.1 and docker --add-host host.docker.internal:host-gateway) or set IDB_HOST"
}

// Apply points go-ios at the resolved endpoint. It is a no-op for the default.
func Apply(flagHost string) string {
	addr := Resolve(flagHost)
	if addr != "" {
		os.Setenv(envMuxd, addr)
	}
	return addr
}

// Local is the usbmuxd address of this machine.
func Local() string {
	if runtime.GOOS == "windows" {
		return "tcp://127.0.0.1:27015"
	}
	return "unix:///var/run/usbmuxd"
}

// normalize accepts "host", "host:port", "tcp://host:port", "unix:///path" or a bare socket path.
func normalize(v string) string {
	if i := strings.Index(v, "://"); i >= 0 {
		return strings.ToLower(v[:i]) + v[i:]
	}
	if strings.HasPrefix(v, "/") {
		return "unix://" + v
	}
	if _, _, err := net.SplitHostPort(v); err != nil {
		v = net.JoinHostPort(strings.Trim(v, "[]"), DefaultPort)
	}
	return "tcp://" + v
}

// Dial connects to an address in go-ios format.
func Dial(addr string) (net.Conn, error) {
	scheme, target, ok := strings.Cut(addr, "://")
	if !ok {
		return nil, fmt.Errorf("invalid usbmuxd address %q", addr)
	}
	return net.Dial(scheme, target)
}
