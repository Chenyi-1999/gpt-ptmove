package migrationflow

import (
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
)

func TestMachineKeyViewStatusAndRevocationPrecedence(t *testing.T) {
	base := repository.MachineKeyRow{
		ID:        "key-id",
		Name:      "automation",
		Scopes:    `["read"]`,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
		CreatedAt: time.Now().Unix(),
	}
	if got := machineKeyView(base)["status"]; got != "active" {
		t.Fatalf("active key status = %v", got)
	}
	expired := base
	expired.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	if got := machineKeyView(expired)["status"]; got != "expired" {
		t.Fatalf("expired key status = %v", got)
	}
	expired.Revoked = true
	view := machineKeyView(expired)
	if got := view["status"]; got != "revoked" {
		t.Fatalf("revoked expired key status = %v, want revoked", got)
	}
	if view["revoked"] != true {
		t.Fatalf("legacy revoked field missing: %#v", view)
	}
}
