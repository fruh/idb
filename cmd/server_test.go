package cmd

import (
	"reflect"
	"testing"

	"github.com/fruh/idb/internal/daemon"
)

func TestSettingsDiffer(t *testing.T) {
	st := daemon.State{Bind: "127.0.0.1", Port: "27016", Upstream: "u", Tunnel: false}
	want := serverCfg{Bind: "127.0.0.1", Port: "27016", Upstream: "u"}
	none := func(string) bool { return false }
	if settingsDiffer(st, want, none) {
		t.Fatal("defaults never differ")
	}
	want.Port = "1"
	if settingsDiffer(st, want, none) {
		t.Fatal("unchanged flags are ignored")
	}
	if !settingsDiffer(st, want, func(n string) bool { return n == "port" }) {
		t.Fatal("explicit different port must differ")
	}
	same := serverCfg{Bind: "127.0.0.1", Port: "27016", Upstream: "u"}
	if settingsDiffer(st, same, func(string) bool { return true }) {
		t.Fatal("explicit but equal flags must not differ")
	}
}

func TestServerArgsAndAlias(t *testing.T) {
	c := serverCfg{Bind: "b", Port: "1", Upstream: "u", Tunnel: true, PairDir: "/p", TunnelPort: 7}
	want := []string{"server", "--foreground", "--bind", "b", "--port", "1", "--upstream", "u", "--tunnel-port", "7", "--tunnel", "--pair-records", "/p"}
	if got := c.args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	cmd, _, err := Root().Find([]string{"start-server"})
	if err != nil || cmd.Name() != "server" {
		t.Fatalf("alias: %v %v", cmd, err)
	}
	if k, _, err := Root().Find([]string{"kill-server"}); err != nil || k.Name() != "kill-server" {
		t.Fatalf("kill-server: %v %v", k, err)
	}
}
