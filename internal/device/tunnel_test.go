package device

import "testing"

func TestTunnelHost(t *testing.T) {
	cases := map[string]string{
		"":                                 "127.0.0.1",
		"unix:///var/run/usbmuxd":          "127.0.0.1",
		"tcp://host.docker.internal:27016": "host.docker.internal",
		"tcp://[::1]:27016":                "::1",
	}
	for in, want := range cases {
		if got := TunnelHost(in); got != want {
			t.Errorf("TunnelHost(%q)=%q want %q", in, got, want)
		}
	}
}
