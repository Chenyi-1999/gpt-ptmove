package fetch

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestScopedFetchKeepsSiteCredentialOnConfiguredOrigin(t *testing.T) {
	var unexpected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unexpected.Add(1)
	}))
	defer other.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer source.Close()
	if _, err := fetchBinaryWithCookieAtOrigin(source.URL, "", "session=fixture", time.Second, source.URL); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{other.URL, source.URL + "/redirect"} {
		if _, err := fetchBinaryWithCookieAtOrigin(address, "", "session=fixture", time.Second, source.URL); err == nil {
			t.Fatal("expected cross-origin fetch rejection")
		}
	}
	if unexpected.Load() != 0 {
		t.Fatal("fetch reached another origin")
	}
}
