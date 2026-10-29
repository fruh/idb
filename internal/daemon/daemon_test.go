//go:build !windows

package daemon

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER") == "1" {
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("HELPER_PORT"))
		if err != nil {
			os.Exit(2)
		}
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}
	os.Exit(m.Run())
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, p, _ := net.SplitHostPort(l.Addr().String())
	return p
}

func TestStateRoundTripAndRemove(t *testing.T) {
	t.Setenv("IDB_HOME", t.TempDir())
	if _, err := Load(); !os.IsNotExist(err) {
		t.Fatalf("want not-exist, got %v", err)
	}
	want := State{PID: 42, Bind: "127.0.0.1", Port: "1", Upstream: "u", Tunnel: true, TunnelPort: 9, Started: time.Now().Truncate(time.Second)}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got.PID != 42 || got.TunnelPort != 9 || !got.Tunnel {
		t.Fatalf("got %+v err %v", got, err)
	}
	Remove(7) // different pid: must not delete
	if _, err := Load(); err != nil {
		t.Fatal("Remove with foreign pid deleted the state")
	}
	Remove(42)
	if _, err := Load(); !os.IsNotExist(err) {
		t.Fatal("state should be gone")
	}
}

func TestRunningRequiresAliveAndListening(t *testing.T) {
	port := freePort(t)
	if (State{PID: 0, Bind: "127.0.0.1", Port: port}).Running() {
		t.Fatal("pid 0 is not running")
	}
	// Alive pid (ourselves) but nothing listening on the port: stale/recycled pid.
	if (State{PID: os.Getpid(), Bind: "127.0.0.1", Port: port}).Running() {
		t.Fatal("live pid without listener must not count as running")
	}
}

func TestStopHelperProcess(t *testing.T) {
	port := freePort(t)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER=1", "HELPER_PORT="+port)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait() // reap so the pid really disappears
	st := State{PID: cmd.Process.Pid, Bind: "127.0.0.1", Port: port}
	deadline := time.Now().Add(3 * time.Second)
	for !st.Running() {
		if time.Now().After(deadline) {
			t.Fatal("helper never became ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Setenv("IDB_HOME", t.TempDir())
	st.Save()
	if err := Stop(st, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if st.Running() {
		t.Fatal("still running after Stop")
	}
	if _, err := Load(); !os.IsNotExist(err) {
		t.Fatal("Stop must remove the state file")
	}
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}
}

func TestAddrWildcard(t *testing.T) {
	if got := (State{Bind: "0.0.0.0", Port: "5"}).Addr(); got != "127.0.0.1:5" {
		t.Fatalf("got %s", got)
	}
}
