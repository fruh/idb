//go:build !windows

package device

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// watchResize forwards terminal size changes to the remote pty.
func watchResize(fd int, s *ssh.Session) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				if w, h, err := term.GetSize(fd); err == nil {
					s.WindowChange(h, w)
				}
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}
