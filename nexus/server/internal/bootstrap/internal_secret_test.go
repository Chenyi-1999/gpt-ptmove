package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInternalSecretPersistsAndRejectsShortOverride(t *testing.T) {
	t.Setenv("INTERNAL_SECRET", "")
	dir := t.TempDir()
	if err := ensureInternalSecret(dir); err != nil {
		t.Fatal(err)
	}
	first := os.Getenv("INTERNAL_SECRET")
	if len(first) != 64 {
		t.Fatal("weak generated internal key")
	}
	os.Setenv("INTERNAL_SECRET", "")
	if err := ensureInternalSecret(dir); err != nil || os.Getenv("INTERNAL_SECRET") != first {
		t.Fatal("key changed across restart")
	}
	if _, err := os.Stat(filepath.Join(dir, "internal-api.key")); err != nil {
		t.Fatal(err)
	}
	os.Setenv("INTERNAL_SECRET", "short")
	if ensureInternalSecret(dir) == nil {
		t.Fatal("short override accepted")
	}
}
