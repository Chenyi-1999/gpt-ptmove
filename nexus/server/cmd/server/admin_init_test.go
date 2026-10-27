package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeAdminNeverPersistsPlaintextOrOverwritesAccount(t *testing.T) {
	dir := t.TempDir()
	input := `{"username":"admin","password":"fixture-only-password-1234"}`
	if initializeAdmin(strings.NewReader(input), dir) != nil {
		t.Fatal("initialization failed")
	}
	path := filepath.Join(dir, "config.json")
	before, _ := os.ReadFile(path)
	if bytes.Contains(before, []byte("fixture-only-password")) {
		t.Fatal("password persisted in plaintext")
	}
	if initializeAdmin(strings.NewReader(input), dir) == nil {
		t.Fatal("existing admin overwritten")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected initialization modified configuration")
	}
}
