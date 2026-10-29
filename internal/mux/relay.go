package mux

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
)

// Serve accepts TCP connections on l and pipes each one to a fresh connection
// to upstream (a usbmuxd address in go-ios format) until ctx is cancelled.
func Serve(ctx context.Context, l net.Listener, upstream string) error {
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go func() {
			if err := pipe(c, upstream); err != nil {
				log.Printf("relay %s: %v", c.RemoteAddr(), err)
			}
		}()
	}
}

func pipe(client net.Conn, upstream string) error {
	defer client.Close()
	up, err := Dial(upstream)
	if err != nil {
		return err
	}
	defer up.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		// Unblock the opposite direction once either side closes.
		dst.Close()
		src.Close()
	}
	go cp(up, client)
	go cp(client, up)
	wg.Wait()
	return nil
}
