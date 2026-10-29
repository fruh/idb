// Package daemon manages the background `idb server`: a state file under
// ~/.idb, a detached child process, and status/stop helpers.
package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fruh/idb/internal/device"
)

const (
	maxLogSize = 5 << 20
	logMarker  = "--- idb server start"
)

// State describes the running server; it is written by the server process
// itself once it is listening.
type State struct {
	PID         int       `json:"pid"`
	Bind        string    `json:"bind"`
	Port        string    `json:"port"`
	Upstream    string    `json:"upstream"`
	Tunnel      bool      `json:"tunnel"`
	TunnelPort  int       `json:"tunnelPort,omitempty"`
	PairRecords string    `json:"pairRecords,omitempty"`
	Started     time.Time `json:"started"`
}

func StatePath() string { return filepath.Join(device.HomeDir(), "server.json") }
func LogPath() string   { return filepath.Join(device.HomeDir(), "server.log") }

// Load returns the recorded state, or an error satisfying os.IsNotExist when there is none.
func Load() (State, error) {
	var s State
	b, err := os.ReadFile(StatePath())
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, fmt.Errorf("corrupt %s: %w", StatePath(), err)
	}
	return s, nil
}

// Save writes the state atomically.
func (s State) Save() error {
	if err := os.MkdirAll(device.HomeDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := StatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, StatePath())
}

// Remove deletes the state file, but only if it still belongs to pid (0 = any).
func Remove(pid int) {
	if pid != 0 {
		if s, err := Load(); err == nil && s.PID != pid {
			return
		}
	}
	os.Remove(StatePath())
}

// Addr is where to dial the server; wildcard binds are dialed via loopback.
func (s State) Addr() string {
	host := s.Bind
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, s.Port)
}

// Running is true only if the process is alive AND its port answers, so a
// recycled pid (e.g. after a reboot) is never mistaken for the server.
func (s State) Running() bool {
	if s.PID <= 0 || !alive(s.PID) {
		return false
	}
	c, err := net.DialTimeout("tcp", s.Addr(), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// Status is the one-line description printed by start-server.
func (s State) Status() string {
	out := fmt.Sprintf("pid %d, %s -> %s", s.PID, net.JoinHostPort(s.Bind, s.Port), s.Upstream)
	if s.Tunnel {
		out += fmt.Sprintf(", tunnel agent :%d", s.TunnelPort)
	}
	if !s.Started.IsZero() {
		out += ", up " + time.Since(s.Started).Round(time.Second).String()
	}
	return out + ", log " + LogPath()
}

// Spawn starts a detached copy of this executable with args, logging to
// LogPath. done receives the child's exit so callers can notice early failure.
func Spawn(args []string) (pid int, done <-chan error, err error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, nil, err
	}
	if err := os.MkdirAll(device.HomeDir(), 0o700); err != nil {
		return 0, nil, err
	}
	if st, err := os.Stat(LogPath()); err == nil && st.Size() > maxLogSize {
		os.Truncate(LogPath(), 0)
	}
	log, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, nil, err
	}
	defer log.Close()
	fmt.Fprintf(log, "%s %s\n", logMarker, time.Now().Format(time.RFC3339))
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, log, log
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}
	ch := make(chan error, 1)
	go func() { ch <- cmd.Wait() }()
	return cmd.Process.Pid, ch, nil
}

// Stop terminates the server (graceful first, forced after timeout) and
// removes its state file.
func Stop(s State, timeout time.Duration) error {
	if err := terminate(s.PID, false); err != nil && alive(s.PID) {
		return err
	}
	if !waitGone(s.PID, timeout) {
		terminate(s.PID, true)
		if !waitGone(s.PID, 2*time.Second) {
			return fmt.Errorf("pid %d did not exit", s.PID)
		}
	}
	Remove(s.PID)
	return nil
}

func waitGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for alive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

// LogTail returns the last n lines of the server log since its latest start.
func LogTail(n int) string {
	b, err := os.ReadFile(LogPath())
	if err != nil {
		return ""
	}
	if i := bytes.LastIndex(b, []byte(logMarker)); i >= 0 {
		b = b[i:] // only the latest start
		if j := bytes.IndexByte(b, '\n'); j >= 0 {
			b = b[j+1:]
		}
	}
	lines := strings.Split(strings.TrimRight(string(bytes.TrimSpace(b)), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ErrNotRunning is returned by helpers when there is no live server.
var ErrNotRunning = errors.New("idb server is not running")
