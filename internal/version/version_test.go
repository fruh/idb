package version

import (
	"strings"
	"testing"
)

func TestStringUsesLdflags(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	defer func() { Version, Commit, Date = oldV, oldC, oldD }()
	Version, Commit, Date = "v1.2.3", "abc1234", "2026-10-29"
	s := String()
	for _, want := range []string{"idb v1.2.3", "commit abc1234", "built 2026-10-29"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
	if Short() != "v1.2.3" {
		t.Errorf("Short() = %q", Short())
	}
}

func TestDefaultIsDev(t *testing.T) {
	oldV := Version
	defer func() { Version = oldV }()
	Version = "dev"
	if got := Short(); got == "" {
		t.Error("empty version")
	}
	if !strings.HasPrefix(String(), "idb ") {
		t.Errorf("String() = %q", String())
	}
}
