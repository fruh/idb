package cmd

import "testing"

func TestParseReverse(t *testing.T) {
	ok := []struct {
		dev, target, want string
		port              uint16
	}{
		{"8080", "8080", "localhost:8080", 8080},
		{"80", "host.docker.internal:9000", "host.docker.internal:9000", 80},
		{"80", "[::1]:9000", "[::1]:9000", 80},
	}
	for _, c := range ok {
		p, tg, err := parseReverse(c.dev, c.target)
		if err != nil || p != c.port || tg != c.want {
			t.Errorf("%v: got %d %q %v", c, p, tg, err)
		}
	}
	for _, c := range [][2]string{{"0", "80"}, {"x", "80"}, {"80", ""}, {"80", "host"}, {"80", "h:0"}, {"70000", "80"}} {
		if _, _, err := parseReverse(c[0], c[1]); err == nil {
			t.Errorf("%v should fail", c)
		}
	}
}
