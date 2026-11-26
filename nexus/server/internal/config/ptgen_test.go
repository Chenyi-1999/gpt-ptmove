package config

import (
	"encoding/json"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

func TestPTGenSettingsPreserveOrderAndRequireExplicitLegacyChoice(t *testing.T) {
	t.Setenv("AUTH_PASSWORD", "qa-only")
	paths := RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")}
	m, err := NewManager(paths)
	if err != nil {
		t.Fatal(err)
	}
	workspace := m.Get()
	crossSeed := workspace["cross_seed"].(map[string]any)
	crossSeed["cspt_ptgen_token"] = "legacy-qa"
	if err := m.Save(workspace); err != nil {
		t.Fatal(err)
	}
	view, err := m.GetPTGenSettings()
	if err != nil || len(view.Sources) != 0 || !view.LegacyCSPTConfigured {
		t.Fatalf("unexpected initial provider list: %+v, %v", view, err)
	}

	managed, err := m.SavePTGenSettings([]PTGenSourceInput{
		{Type: "auto", URL: "https://a.example.org/api/v2/info", Token: "isolated-first"},
		{Type: "cspt", URL: "https://cspt.top", UseLegacyCSPTToken: true},
		{Type: "ourbits", URL: "https://api.ourhelp.club/infogen"},
	})
	if err != nil || len(managed.Sources) != 3 || !managed.Sources[1].TokenConfigured {
		t.Fatalf("provider save: %+v, %v", managed, err)
	}
	viewJSON, err := json.Marshal(managed)
	if err != nil || strings.Contains(string(viewJSON), "legacy-qa") || strings.Contains(string(viewJSON), "isolated-first") {
		t.Fatal("response did not redact provider tokens")
	}
	ordered, err := m.SavePTGenSettings([]PTGenSourceInput{
		{ID: managed.Sources[2].ID, Type: "ourbits", URL: managed.Sources[2].URL},
		{ID: managed.Sources[0].ID, Type: "universal", URL: managed.Sources[0].URL},
		{ID: managed.Sources[1].ID, Type: "cspt", URL: managed.Sources[1].URL},
	})
	if err != nil || ordered.Sources[0].ResolvedType != "ourbits" || ordered.Sources[1].ResolvedType != "universal" {
		t.Fatalf("provider reorder: %+v, %v", ordered, err)
	}
	// A general settings save must not write an earlier view of the provider list.
	if err := m.Save(workspace); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(paths)
	if err != nil {
		t.Fatal(err)
	}
	stored := reopened.PTGenSources()
	if len(stored) != 3 || stored[1].Token != "isolated-first" || stored[2].Token != "legacy-qa" {
		t.Fatal("reorder and service restart did not preserve the related credentials")
	}

	changed := PTGenSourceInput{ID: stored[1].ID, Type: "universal", URL: "https://b.example.org/api/v2/info"}
	if _, err = reopened.SavePTGenSettings([]PTGenSourceInput{changed}); err == nil {
		t.Fatal("credential must not move to another address")
	}
	if len(reopened.PTGenSources()) != 3 {
		t.Fatal("failed save changed the stored provider list")
	}
	changed.ClearToken = true
	saved, err := reopened.SavePTGenSettings([]PTGenSourceInput{changed})
	if err != nil || len(saved.Sources) != 1 || saved.Sources[0].TokenConfigured {
		t.Fatalf("explicit credential clear failed: %+v, %v", saved, err)
	}
}

func TestResolvePTGenTypesAndRejectUnsafeAddresses(t *testing.T) {
	for _, test := range []struct{ kind, endpoint, want string }{
		{"auto", "https://cspt.top/", "cspt"},
		{"auto", "https://api.example.net/api/v2/info", "universal"},
		{"auto", "https://ourbits.github.io/PtGen/", "ourbits"},
		{"auto", "https://api.ourhelp.club/infogen", "ourbits"},
		{"generic", "https://ptgen.example.net/", "generic"},
		{"auto", "https://ptgen.example.net/", ""},
		{"cspt", "https://cspt.top.example.org/", ""},
		{"generic", "https://user:password@example.net/", ""},
		{"generic", "https://example.net/?token=example", ""},
		{"generic", "https://81.70.24.5/api", ""},
		{"generic", "https://127.0.0.1/api", ""},
		{"generic", "https://[::ffff:81.70.24.5]/api", ""},
		{"generic", "http://example.net/api", ""},
	} {
		if got, err := ResolvePTGenType(test.kind, test.endpoint); got != test.want || (err != nil) != (test.want == "") {
			t.Errorf("kind %q / URL %q gave %q (%v), want %q", test.kind, test.endpoint, got, err, test.want)
		}
	}
	for _, ip := range []string{"10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.0.10", "240.0.0.1",
		"2002:5146:1805::1", "64:ff9b::5146:1805", "::81.70.24.5", "::1"} {
		if IsPTGenPublicAddress(netip.MustParseAddr(ip)) {
			t.Errorf("accepted unsafe address %s", ip)
		}
	}
}
