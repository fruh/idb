// Package device is the only place that talks to go-ios. Everything else in
// idb goes through these functions so the backend can be swapped.
package device

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/golog"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/danielpaulus/go-ios/ios/syslog"
	"github.com/danielpaulus/go-ios/ios/zipconduit"
)

// Info is the subset of device facts idb prints.
type Info struct {
	UDID       string `json:"udid"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	ProductVer string `json:"ios_version"`
	Connection string `json:"connection"`
}

// App describes an installed application.
type App struct {
	BundleID  string `json:"bundle_id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Type      string `json:"type"`
	Path      string `json:"path"`
	Container string `json:"container,omitempty"`
}

// SetLogLevel controls go-ios internal logging (it is chatty at info level).
func SetLogLevel(verbose bool) {
	lvl := slog.LevelError
	if verbose {
		lvl = slog.LevelDebug
	}
	golog.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

// Select resolves a device by UDID; an empty UDID is allowed only when exactly
// one device is attached, mirroring adb.
func Select(udid string) (ios.DeviceEntry, error) {
	if udid != "" {
		return ios.GetDevice(udid)
	}
	list, err := ios.ListDevices()
	if err != nil {
		return ios.DeviceEntry{}, err
	}
	switch n := len(list.DeviceList); n {
	case 0:
		return ios.DeviceEntry{}, fmt.Errorf("no devices found (is it plugged in, unlocked and trusted?)")
	case 1:
		return list.DeviceList[0], nil
	default:
		return ios.DeviceEntry{}, fmt.Errorf("%d devices attached, pick one with -s <udid> or IDB_SERIAL", n)
	}
}

// List returns all attached devices. With details it also queries lockdown for
// name, model and iOS version; devices that are locked or untrusted still appear.
func List(details bool) ([]Info, error) {
	list, err := ios.ListDevices()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(list.DeviceList))
	for _, d := range list.DeviceList {
		in := Info{UDID: d.Properties.SerialNumber, Connection: d.Properties.ConnectionType}
		if details {
			if v, err := ios.GetValuesPlist(d); err == nil {
				in.Name, _ = v["DeviceName"].(string)
				in.Model, _ = v["ProductType"].(string)
				in.ProductVer, _ = v["ProductVersion"].(string)
			}
		}
		out = append(out, in)
	}
	return out, nil
}

// Values returns the full lockdown dictionary (`idb info`).
func Values(d ios.DeviceEntry) (map[string]any, error) {
	return ios.GetValuesPlist(d)
}

// Apps lists installed apps. kind is "user", "system" or "all".
func Apps(d ios.DeviceEntry, kind string) ([]App, error) {
	conn, err := installationproxy.New(d)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var infos []installationproxy.AppInfo
	switch kind {
	case "user":
		infos, err = conn.BrowseUserApps()
	case "system":
		infos, err = conn.BrowseSystemApps()
	case "all":
		infos, err = conn.BrowseAllApps()
	default:
		return nil, fmt.Errorf("unknown app kind %q (user|system|all)", kind)
	}
	if err != nil {
		return nil, err
	}
	apps := make([]App, 0, len(infos))
	for _, i := range infos {
		a := App{
			BundleID: i.CFBundleIdentifier(),
			Name:     i.CFBundleName(),
			Version:  i.CFBundleShortVersionString(),
			Path:     i.Path(),
		}
		a.Type, _ = i["ApplicationType"].(string)
		a.Container, _ = i["Container"].(string)
		apps = append(apps, a)
	}
	sort.Slice(apps, func(x, y int) bool { return apps[x].BundleID < apps[y].BundleID })
	return apps, nil
}

// Install pushes an .ipa (or app directory) to the device and installs it.
func Install(d ios.DeviceEntry, path string) error {
	conn, err := zipconduit.New(d)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.SendFile(path)
}

// Uninstall removes an app by bundle id.
func Uninstall(d ios.DeviceEntry, bundleID string) error {
	conn, err := installationproxy.New(d)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Uninstall(bundleID)
}

// Syslog streams device log lines to w until the connection drops. keep, if
// non-nil, filters raw lines.
func Syslog(d ios.DeviceEntry, w io.Writer, keep func(line string) bool) error {
	conn, err := syslog.New(d)
	if err != nil {
		return err
	}
	for {
		line, err := conn.ReadLogMessage()
		if err != nil {
			return err
		}
		if keep == nil || keep(line) {
			fmt.Fprintln(w, trimNL(line))
		}
	}
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == 0) {
		s = s[:len(s)-1]
	}
	return s
}
