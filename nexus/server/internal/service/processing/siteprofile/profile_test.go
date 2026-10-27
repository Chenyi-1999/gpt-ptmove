package siteprofile

import (
	"encoding/json"
	"strings"
	"testing"
)

func validProfileJSON(t *testing.T) []byte {
	t.Helper()
	p := Default()
	p.ID = "fixture"
	p.Name = "Fixture"
	p.BaseURL = "https://tracker.example.com"
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestProfileStrictValidation(t *testing.T) {
	raw := validProfileJSON(t)
	p, e := Parse(raw)
	if e != nil || p.Digest == "" || p.Health != "declared/unverified" {
		t.Fatalf("valid declaration: %v %+v", e, p)
	}
	cases := map[string]string{"unknown": `"script":"alert(1)"`, "credential": `"cookie":"secret"`, "metadata": `"revision":3`, "nullpaths": `"paths":null`, "nullselector": `"selectors":{"title":null}`}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			base := `{"id":"fixture","name":"Fixture","base_url":"https://tracker.example.com","roles":["source"],` + extra + `}`
			if _, e := Parse([]byte(base)); e == nil {
				t.Fatal("accepted unsafe profile")
			}
		})
	}
	for _, origin := range []string{"http://tracker.example.com", "https://81.70.24.5", "https://127.1", "https://user:secret@tracker.example.com", "https://tracker.example.com/path", "https://tracker.local", "https://tracker.example.com?token=x"} {
		t.Run(origin, func(t *testing.T) {
			p := Default()
			p.ID = "fixture"
			p.Name = "Fixture"
			p.BaseURL = origin
			if Validate(&p) == nil {
				t.Fatal("accepted unsafe origin")
			}
		})
	}
	for _, path := range []string{"//evil.example.com/{id}", "/../details.php?id={id}", "/%2e%2e/details.php?id={id}", "/details.php?id={id}&passkey=secret", "/details.php?id={other}", "/details.php?id={id}{id}"} {
		p := Default()
		p.ID = "fixture"
		p.Name = "Fixture"
		p.BaseURL = "https://tracker.example.com"
		p.Paths.Detail = path
		if Validate(&p) == nil {
			t.Fatalf("accepted path %s", path)
		}
	}
	if _, e := Parse([]byte(strings.Repeat(" ", 65537))); e == nil {
		t.Fatal("accepted oversized declaration")
	}
	if _, e := Parse([]byte(`{"id":"fixture","name":"Fixture","base_url":"https://tracker.example.com"}`)); e == nil {
		t.Fatal("roles required")
	}
}
func TestProfileURLAndPublicMetadata(t *testing.T) {
	p, e := Parse(validProfileJSON(t))
	if e != nil {
		t.Fatal(e)
	}
	u, e := p.URL("detail", "a&passkey=secret")
	if e != nil || strings.Contains(u, "&passkey=") {
		t.Fatalf("parameter escaped incorrectly: %s %v", u, e)
	}
	p.NativeNickname = "private nickname"
	p.NativeSiteCode = "native"
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "private nickname") || p.NativeName() != "private nickname" || p.NativeCode() != "native" {
		t.Fatal("internal native identity leaked or misrouted")
	}
}
