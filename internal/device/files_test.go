package device

import "testing"

func TestJoin(t *testing.T) {
	cases := []struct{ cwd, p, want string }{
		{"/", "", "/"},
		{"/Documents", "a.txt", "/Documents/a.txt"},
		{"/Documents", "/Library", "/Library"},
		{"/Documents/x", "../..", "/"},
		{"/", "../../etc", "/etc"},
	}
	for _, c := range cases {
		if got := Join(c.cwd, c.p); got != c.want {
			t.Errorf("Join(%q,%q)=%q want %q", c.cwd, c.p, got, c.want)
		}
	}
}
