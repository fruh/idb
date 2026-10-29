package device

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/imagemounter"
	"github.com/danielpaulus/go-ios/ios/instruments"
)

// Process is a running process on the device.
type Process struct {
	PID         uint64    `json:"pid"`
	Name        string    `json:"name"`
	Application bool      `json:"application"`
	Started     time.Time `json:"started"`
}

// Processes lists running processes (developer services).
func Processes(d ios.DeviceEntry) ([]Process, error) {
	s, err := instruments.NewDeviceInfoService(d)
	if err != nil {
		return nil, devErr(err)
	}
	defer s.Close()
	list, err := s.ProcessList()
	if err != nil {
		return nil, err
	}
	out := make([]Process, 0, len(list))
	for _, p := range list {
		out = append(out, Process{PID: p.Pid, Name: p.Name, Application: p.IsApplication, Started: p.StartDate})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

// Launch starts an app and returns its pid. With killExisting, a running
// instance is terminated first.
func Launch(d ios.DeviceEntry, bundleID string, args []string, env map[string]string, killExisting bool) (uint64, error) {
	pc, err := instruments.NewProcessControl(d)
	if err != nil {
		return 0, devErr(err)
	}
	defer pc.Close()
	a := make([]interface{}, len(args))
	for i, s := range args {
		a[i] = s
	}
	e := map[string]any{}
	for k, v := range env {
		e[k] = v
	}
	opts := map[string]any{}
	if killExisting {
		opts["KillExisting"] = uint64(1)
	}
	return pc.LaunchAppWithArgs(bundleID, a, e, opts)
}

// Kill terminates a process by pid.
func Kill(d ios.DeviceEntry, pid uint64) error {
	pc, err := instruments.NewProcessControl(d)
	if err != nil {
		return devErr(err)
	}
	defer pc.Close()
	return pc.KillProcess(pid)
}

// KillByName terminates the process with the given executable name or, for
// an app, its bundle id's executable.
func KillByName(d ios.DeviceEntry, name string) (uint64, error) {
	procs, err := Processes(d)
	if err != nil {
		return 0, err
	}
	for _, p := range procs {
		if p.Name == name {
			return p.PID, Kill(d, p.PID)
		}
	}
	return 0, fmt.Errorf("no process named %q", name)
}

// ImageCacheDir is where developer images are cached: IDB_IMAGES, else
// <HomeDir>/images (~/.idb/images).
func ImageCacheDir() string {
	if d := os.Getenv("IDB_IMAGES"); d != "" {
		return d
	}
	return filepath.Join(HomeDir(), "images")
}

// MountImage downloads the matching image into cacheDir when missing and
// mounts it. Downloads are serialized with a lock file so concurrent idb runs
// don't corrupt a half-written image.
func MountImage(d ios.DeviceEntry, cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	unlock, err := LockDir(cacheDir)
	if err != nil {
		return err
	}
	path, err := imagemounter.DownloadImageFor(d, cacheDir)
	unlock()
	if err != nil {
		return fmt.Errorf("fetch developer image: %w (the download needs internet access on the machine running idb)", err)
	}
	if err := imagemounter.MountImage(d, path); err != nil {
		return fmt.Errorf("mount developer image: %w (iOS 16+ also needs Developer Mode: Settings > Privacy & Security > Developer Mode; keep the device unlocked)", err)
	}
	return nil
}

// EnsureImage mounts a developer image if none is mounted, telling the user
// what it does through notify. A failing mounted-check is not fatal: the
// command that follows reports its own error.
func EnsureImage(d ios.DeviceEntry, cacheDir string, notify func(format string, a ...any)) error {
	ver := "?"
	if v, err := ios.GetProductVersion(d); err == nil {
		ver = v.String()
	}
	return ensureImage(
		func() (bool, error) { return ImageMounted(d) },
		func() error { return MountImage(d, cacheDir) },
		notify, d.Properties.SerialNumber, ver, cacheDir)
}

func ensureImage(mounted func() (bool, error), mount func() error, notify func(string, ...any), udid, ver, cacheDir string) error {
	ok, err := mounted()
	if err != nil || ok {
		return nil
	}
	notify("no developer image mounted on %s (iOS %s); downloading to %s and mounting...", udid, ver, cacheDir)
	if err := mount(); err != nil {
		return err
	}
	notify("developer image mounted")
	return nil
}

const (
	lockWait  = 10 * time.Minute
	lockStale = 15 * time.Minute
)

// LockDir takes <dir>/.lock (O_EXCL); a lock older than lockStale is taken over.
func LockDir(dir string) (func(), error) {
	path := filepath.Join(dir, ".lock")
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if st, serr := os.Stat(path); serr == nil && time.Since(st.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another idb is downloading into %s (remove %s if it is stale)", dir, path)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ImageMounted reports whether a developer image is mounted.
func ImageMounted(d ios.DeviceEntry) (bool, error) {
	m, err := imagemounter.NewImageMounter(d)
	if err != nil {
		return false, err
	}
	defer m.Close()
	sigs, err := m.ListImages()
	return len(sigs) > 0, err
}

func devErr(err error) error {
	return fmt.Errorf("%w (developer services need a mounted developer image (idb mounts it automatically unless --no-auto-mount is set; or run `idb mount`); on iOS 17+ also `idb server --tunnel` on the host and Developer Mode enabled)", err)
}
