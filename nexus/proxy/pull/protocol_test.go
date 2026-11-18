package main

import (
	"context"
	"net/http"
	"testing"
)

func TestCoordinatorDialAddressProtection(t *testing.T) {
	for _, address := range []string{"81.70.24.5:28443", "[::ffff:81.70.24.5]:28443"} {
		if err := protectAgentCoordinator(context.Background(), "tcp", address, nil); err == nil {
			t.Errorf("coordinator dial to protected address %s would be allowed", address)
		}
	}
	if err := protectAgentCoordinator(context.Background(), "tcp", "152.136.236.238:28443", nil); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownScreenshotModeCannotStartUpload(t *testing.T) {
	job := Job{
		Lane:        "media",
		Method:      http.MethodPost,
		URI:         "/api/media/screenshot",
		ContentType: "application/json",
		Body:        []byte(`{"remote_path":"/keep/example.mkv","mode":"typo"}`),
	}
	if err := validateJob(job); err == nil {
		t.Fatal("unexpected screenshot mode could reach the NAS")
	}
	for _, mode := range []string{"", "inspect", "preview", "finalize"} {
		job.Body = []byte(`{"remote_path":"/keep/example.mkv","mode":"` + mode + `"}`)
		if err := validateJob(job); err != nil {
			t.Errorf("expected screenshot mode %q to be accepted: %v", mode, err)
		}
	}
}
