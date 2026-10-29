package device

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureImage(t *testing.T) {
	var notes []string
	notify := func(f string, a ...any) { notes = append(notes, fmt.Sprintf(f, a...)) }

	mounts := 0
	mount := func() error { mounts++; return nil }
	if err := ensureImage(func() (bool, error) { return true, nil }, mount, notify, "U", "16.7", "/c"); err != nil || mounts != 0 || len(notes) != 0 {
		t.Fatalf("already mounted: err=%v mounts=%d notes=%v", err, mounts, notes)
	}
	if err := ensureImage(func() (bool, error) { return false, nil }, mount, notify, "U", "16.7", "/c"); err != nil || mounts != 1 || len(notes) != 2 {
		t.Fatalf("not mounted: err=%v mounts=%d notes=%v", err, mounts, notes)
	}
	if !strings.Contains(notes[0], "downloading to /c") {
		t.Fatalf("notice: %q", notes[0])
	}

	mounts, notes = 0, nil
	if err := ensureImage(func() (bool, error) { return false, errors.New("locked") }, mount, notify, "U", "16.7", "/c"); err != nil || mounts != 0 {
		t.Fatalf("check error must not block: err=%v mounts=%d", err, mounts)
	}
	boom := errors.New("boom")
	if err := ensureImage(func() (bool, error) { return false, nil }, func() error { return boom }, notify, "U", "16.7", "/c"); !errors.Is(err, boom) {
		t.Fatalf("mount error must propagate: %v", err)
	}
}

func TestImageCacheDir(t *testing.T) {
	t.Setenv("IDB_IMAGES", "/custom")
	if got := ImageCacheDir(); got != "/custom" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("IDB_IMAGES", "")
	t.Setenv("IDB_HOME", "/state")
	if got := ImageCacheDir(); got != filepath.Join("/state", "images") {
		t.Fatalf("IDB_HOME: got %q", got)
	}
	t.Setenv("IDB_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got := ImageCacheDir(); got != filepath.Join(home, ".idb", "images") {
		t.Fatalf("got %q", got)
	}
	if got := HomeDir(); got != filepath.Join(home, ".idb") {
		t.Fatalf("HomeDir got %q", got)
	}
}

func TestLockDir(t *testing.T) {
	dir := t.TempDir()
	unlock, err := LockDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A stale lock is taken over.
	unlock()
	lock := filepath.Join(dir, ".lock")
	os.WriteFile(lock, nil, 0o644)
	old := time.Now().Add(-2 * lockStale)
	os.Chtimes(lock, old, old)
	unlock2, err := LockDir(dir)
	if err != nil {
		t.Fatalf("stale lock must be taken over: %v", err)
	}
	unlock2()
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("unlock must remove the lock")
	}
}
