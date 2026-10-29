//go:build windows

package device

import "golang.org/x/crypto/ssh"

// watchResize is a no-op on Windows (no SIGWINCH).
func watchResize(int, *ssh.Session) func() { return func() {} }
