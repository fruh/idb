package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fruh/idb/internal/device"
)

func TestShouldFallback(t *testing.T) {
	noSSH := fmt.Errorf("x: %w", device.ErrNoSSH)
	cases := []struct {
		name  string
		err   error
		force bool
		cmd   []string
		want  bool
	}{
		{"no sshd interactive", noSSH, false, nil, true},
		{"forced ssh", noSSH, true, nil, false},
		{"command given", noSSH, false, []string{"id"}, false},
		{"login failure", errors.New("unable to authenticate"), false, nil, false},
	}
	for _, c := range cases {
		if got := shouldFallback(c.err, c.force, c.cmd); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
