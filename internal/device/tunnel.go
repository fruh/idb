package device

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/tunnel"

	"github.com/fruh/idb/internal/mux"
)

// TunnelCfg says where the tunnel agent (started by `idb server --tunnel`) lives.
type TunnelCfg struct {
	Host string
	Port int
}

// TunnelHost derives the agent host from the resolved usbmuxd address: the
// agent runs next to usbmuxd, so a remote usbmuxd means a remote agent.
func TunnelHost(muxAddr string) string {
	if rest, ok := strings.CutPrefix(muxAddr, "tcp://"); ok {
		if h, _, err := net.SplitHostPort(rest); err == nil {
			return h
		}
	}
	return "127.0.0.1"
}

// SelectDeveloper returns a device usable for developer services (DVT,
// instruments, screenshots). iOS 17+ needs a tunnel from the agent; older
// versions only need the developer disk image, so they pass through.
func SelectDeveloper(udid string, cfg TunnelCfg) (ios.DeviceEntry, error) {
	d, err := Select(udid)
	if err != nil {
		return d, err
	}
	ver, err := ios.GetProductVersion(d)
	if err != nil {
		return d, err
	}
	if ver.Major() < 17 {
		return d, nil
	}
	info, err := tunnel.TunnelInfoForDevice(d.Properties.SerialNumber, cfg.Host, cfg.Port)
	if err != nil {
		return d, fmt.Errorf("iOS %s needs a tunnel for this command and none was found at %s:%d (%v); run `idb server --tunnel` on the host", ver, cfg.Host, cfg.Port, err)
	}
	rsd, err := ios.NewWithAddrPortDevice(info.Address, info.RsdPort, withUserspace(d, cfg.Host, info))
	if err != nil {
		return d, fmt.Errorf("connect to RSD at %s:%d: %w", info.Address, info.RsdPort, err)
	}
	defer rsd.Close()
	provider, err := rsd.Handshake()
	if err != nil {
		return d, fmt.Errorf("RSD handshake: %w", err)
	}
	dev, err := ios.GetDeviceWithAddress(d.Properties.SerialNumber, info.Address, provider)
	if err != nil {
		return d, err
	}
	dev.UserspaceTUN = info.UserspaceTUN
	dev.UserspaceTUNHost = cfg.Host
	dev.UserspaceTUNPort = info.UserspaceTUNPort
	return dev, nil
}

func withUserspace(d ios.DeviceEntry, host string, info tunnel.Tunnel) ios.DeviceEntry {
	d.UserspaceTUN = info.UserspaceTUN
	d.UserspaceTUNHost = host
	d.UserspaceTUNPort = info.UserspaceTUNPort
	return d
}

// RunTunnelAgent runs the iOS 17+ tunnel manager with userspace networking
// (no root needed) and serves its info API on host:port until ctx ends. When
// relayBind is a specific non-loopback address, the per-device userspace
// listener ports, which go-ios binds to localhost only, are relayed there so
// containers can reach them.
func RunTunnelAgent(ctx context.Context, pairRecords, host string, port int, relayBind string) error {
	if err := os.MkdirAll(pairRecords, 0o700); err != nil {
		return err
	}
	pm, err := tunnel.NewPairRecordManager(pairRecords)
	if err != nil {
		return fmt.Errorf("pair record manager: %w", err)
	}
	tm := tunnel.NewTunnelManager(pm, true)

	errc := make(chan error, 1)
	go func() { errc <- tunnel.ServeTunnelInfo(tm, host, port) }()

	var relays relaySet
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			relays.closeAll()
			return nil
		case err := <-errc:
			return err
		case <-tick.C:
			if err := tm.UpdateTunnels(ctx); err != nil {
				log.Printf("tunnel update: %v", err)
			}
			if relayBind != "" {
				ts, _ := tm.ListTunnels()
				for _, t := range ts {
					if t.UserspaceTUN {
						relays.ensure(ctx, relayBind, t.UserspaceTUNPort)
					}
				}
			}
		}
	}
}

type relaySet struct {
	mu sync.Mutex
	ls map[int]net.Listener
}

func (r *relaySet) ensure(ctx context.Context, bind string, port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ls[port]; ok {
		return
	}
	l, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		log.Printf("tunnel relay :%d: %v", port, err)
		return
	}
	if r.ls == nil {
		r.ls = map[int]net.Listener{}
	}
	r.ls[port] = l
	go mux.Serve(ctx, l, "tcp://127.0.0.1:"+strconv.Itoa(port))
}

func (r *relaySet) closeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.ls {
		l.Close()
	}
}

// ErrNoDeveloper is returned when developer services are unavailable.
var ErrNoDeveloper = errors.New("developer services unavailable")
