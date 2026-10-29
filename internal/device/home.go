package device

import (
	"os"
	"path/filepath"
)

// HomeDir is where idb keeps its state (known_hosts, images/, pair/):
// IDB_HOME, else ~/.idb, else a directory under the OS temp dir.
func HomeDir() string {
	if d := os.Getenv("IDB_HOME"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".idb")
	}
	return filepath.Join(os.TempDir(), "idb")
}
