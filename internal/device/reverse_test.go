package device

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// reverseSSHD accepts the login and the tcpip-forward request, then opens a
// forwarded-tcpip channel for every value sent on trigger.
func reverseSSHD(t *testing.T, c net.Conn, trigger <-chan struct{}, got chan<- []byte, drop <-chan struct{}) {
	cfg := &ssh.ServerConfig{PasswordCallback: func(m ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		return nil, nil
	}}
	cfg.AddHostKey(testSigner(t))
	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(chans2reject(chans))
	var bind string
	var port uint32
	go func() {
		<-drop
		sc.Close()
	}()
	for r := range reqs {
		if r.Type != "tcpip-forward" {
			r.Reply(false, nil)
			continue
		}
		l := binary.BigEndian.Uint32(r.Payload)
		bind = string(r.Payload[4 : 4+l])
		port = binary.BigEndian.Uint32(r.Payload[4+l:])
		r.Reply(true, nil)
		go func() {
			for range trigger {
				p := ssh.Marshal(struct {
					Addr       string
					Port       uint32
					OriginAddr string
					OriginPort uint32
				}{bind, port, "127.0.0.1", 50000})
				ch, creqs, err := sc.OpenChannel("forwarded-tcpip", p)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(creqs)
				ch.Write([]byte("ping"))
				b := make([]byte, 4)
				io.ReadFull(ch, b)
				got <- b
				ch.Close()
			}
		}()
	}
}

func chans2reject(chans <-chan ssh.NewChannel) <-chan *ssh.Request {
	go func() {
		for nc := range chans {
			nc.Reject(ssh.Prohibited, "no")
		}
	}()
	return make(chan *ssh.Request)
}

func TestReverseRoundTrip(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				b := make([]byte, 4)
				if _, err := io.ReadFull(c, b); err == nil {
					c.Write([]byte(strings.ToUpper(string(b))))
				}
			}()
		}
	}()

	cli, srv := tcpPair(t)
	trigger, got, drop := make(chan struct{}), make(chan []byte, 1), make(chan struct{})
	go reverseSSHD(t, srv, trigger, got, drop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, res := make(chan struct{}), make(chan error, 1)
	cfg := SSHConfig{User: "mobile", Password: "x", KnownHostsPath: filepath.Join(t.TempDir(), "kh")}
	go func() { res <- reverse(ctx, cli, "UDID", cfg, "127.0.0.1", 8080, echo.Addr().String(), ready) }()

	select {
	case <-ready:
	case err := <-res:
		t.Fatalf("reverse returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("not listening")
	}
	trigger <- struct{}{}
	select {
	case b := <-got:
		if string(b) != "PING" {
			t.Fatalf("round trip got %q", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no data came back")
	}

	// dropping the session ends reverse with an error
	close(drop)
	select {
	case err := <-res:
		if err == nil || !strings.Contains(err.Error(), "ssh session closed") {
			t.Fatalf("want session closed error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reverse did not return after the session dropped")
	}
}

func TestReverseStopsOnContext(t *testing.T) {
	cli, srv := tcpPair(t)
	go reverseSSHD(t, srv, make(chan struct{}), make(chan []byte, 1), make(chan struct{}))
	ctx, cancel := context.WithCancel(context.Background())
	ready, res := make(chan struct{}), make(chan error, 1)
	cfg := SSHConfig{User: "mobile", Password: "x", KnownHostsPath: filepath.Join(t.TempDir(), "kh")}
	go func() { res <- reverse(ctx, cli, "UDID", cfg, "127.0.0.1", 8081, "127.0.0.1:1", ready) }()
	<-ready
	cancel()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("cancel should be a clean stop, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reverse did not stop")
	}
}
