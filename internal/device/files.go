package device

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/afc"
	"github.com/danielpaulus/go-ios/ios/crashreport"
	"github.com/danielpaulus/go-ios/ios/forward"
	"github.com/danielpaulus/go-ios/ios/house_arrest"
	"github.com/danielpaulus/go-ios/ios/instruments"
)

// FS is a remote file system: either the device media partition (AFC) or one
// app's container (house_arrest). Paths are slash-separated and relative to its root.
type FS struct {
	*afc.Client
	Label string
}

// OpenFS opens the media partition when bundleID is empty, otherwise the app's container.
func OpenFS(d ios.DeviceEntry, bundleID string) (*FS, error) {
	if bundleID == "" {
		c, err := afc.New(d)
		if err != nil {
			return nil, err
		}
		return &FS{Client: c, Label: "media"}, nil
	}
	c, err := house_arrest.New(d, bundleID)
	if err != nil {
		return nil, err
	}
	return &FS{Client: c, Label: bundleID}, nil
}

// Entry is one directory listing row.
type Entry struct {
	Name string
	Info afc.FileInfo
}

// ReadDir lists dir with stat info, directories first.
func (f *FS) ReadDir(dir string) ([]Entry, error) {
	names, err := f.List(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []Entry
	for _, n := range names {
		info, err := f.Stat(path.Join(dir, n))
		if err != nil {
			info = afc.FileInfo{Name: n}
		}
		e := Entry{Name: n, Info: info}
		if info.IsDir() {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	return append(dirs, files...), nil
}

// Cat copies a remote file to w.
func (f *FS) Cat(p string, w io.Writer) error {
	fd, err := f.Open(p, afc.READ_ONLY)
	if err != nil {
		return err
	}
	defer fd.Close()
	_, err = io.Copy(w, fd)
	return err
}

// Join resolves p against cwd the way a shell would, anchored at "/".
func Join(cwd, p string) string {
	if p == "" {
		return cwd
	}
	if !strings.HasPrefix(p, "/") {
		p = path.Join(cwd, p)
	}
	return path.Clean("/" + p)
}

// Crashlogs downloads crash reports matching pattern into dir.
func Crashlogs(d ios.DeviceEntry, pattern, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return crashreport.DownloadReports(d, pattern, dir)
}

// ListCrashlogs returns crash report names matching pattern.
func ListCrashlogs(d ios.DeviceEntry, pattern string) ([]string, error) {
	return crashreport.ListReports(d, pattern)
}

// Screenshot returns image bytes. It needs developer services, which on
// iOS 17+ means a tunnel is required.
func Screenshot(d ios.DeviceEntry) ([]byte, error) {
	s, err := instruments.NewScreenshotService(d)
	if err != nil {
		return nil, fmt.Errorf("%w (screenshots need the developer disk image; on iOS 17+ also a tunnel)", err)
	}
	defer s.Close()
	return s.TakeScreenshot()
}

// Forward listens on hostPort and relays each connection to phonePort on the device.
// The returned function stops the listener.
func Forward(d ios.DeviceEntry, hostPort, phonePort uint16) (func() error, error) {
	l, err := forward.Forward(d, hostPort, phonePort)
	if err != nil {
		return nil, err
	}
	return l.Close, nil
}
