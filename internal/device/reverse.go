package device

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/danielpaulus/go-ios/ios"
)

// dialTargetFn is replaced in tests.
var dialTargetFn = func(target string) (net.Conn, error) { return net.Dial("tcp", target) }

// Reverse makes sshd on a jailbroken device listen on bind:devicePort and sends
// every connection back through the SSH session to target, dialled from where
// idb runs. It blocks until ctx is done or the SSH session ends.
func Reverse(ctx context.Context, d ios.DeviceEntry, cfg SSHConfig, bind string, devicePort uint16, target string) error {
	conn, err := dialSSH(d, &cfg)
	if err != nil {
		return err
	}
	return reverse(ctx, conn, d.Properties.SerialNumber, cfg, bind, devicePort, target, nil)
}

// reverse runs the forward over an established connection; ready (if set) is
// closed once the device side is listening.
func reverse(ctx context.Context, conn net.Conn, udid string, cfg SSHConfig, bind string, devicePort uint16, target string, ready chan<- struct{}) error {
	client, closeAgent, err := connectSSH(conn, udid, cfg)
	if err != nil {
		return err
	}
	defer closeAgent()
	ln, err := client.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(int(devicePort))))
	if err != nil {
		client.Close()
		return fmt.Errorf("device refused to listen on %s:%d (sshd needs AllowTcpForwarding, and the port must be free): %w", bind, devicePort, err)
	}
	// ln.Close sends cancel-tcpip-forward and waits for sshd's reply, which can
	// hang forever on a dead session, so bound it and always close the client after.
	defer func() {
		closed := make(chan struct{})
		go func() { ln.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
		}
		client.Close()
	}()
	if ready != nil {
		close(ready)
	}

	done := make(chan error, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				done <- err
				return
			}
			go serveReverse(c, target)
		}
	}()
	go func() { done <- client.Wait() }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("ssh session closed: %w", err)
	}
}

func serveReverse(c net.Conn, target string) {
	defer c.Close()
	t, err := dialTargetFn(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "idb: reverse: cannot reach %s: %v\n", target, err)
		return
	}
	defer t.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		} else {
			dst.Close()
		}
	}
	go cp(t, c)
	go cp(c, t)
	wg.Wait()
}
