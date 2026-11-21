package migrationflow

import (
	"strings"
	"testing"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

func TestMaintenanceSourceProof(t *testing.T) {
	makeTorrent := func(source, piece string) []byte {
		return []byte("d8:announce35:https://source.example.com/announce4:infod6:lengthi4e4:name4:test12:piece lengthi4e6:pieces20:" +
			strings.Repeat(piece, 20) + "7:privatei1e6:source1:" + source + "ee")
	}
	donor, target, different := makeTorrent("A", "a"), makeTorrent("B", "a"), makeTorrent("B", "b")
	meta, err := acquirefetch.ParseTorrentMeta(donor)
	if err != nil {
		t.Fatal(err)
	}
	targetMeta, err := acquirefetch.ParseTorrentMeta(target)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedMeta, err := acquirefetch.ParseTorrentMeta(different)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, status, site, id, hash string
		loadData                     []byte
		allowed                      bool
		inspectCalls, loadCalls      int
	}{
		{"same record", "denied", "a", "1", meta.InfoHash, nil, true, 0, 0},
		{"changed own hash", "allowed", "a", "1", targetMeta.InfoHash, nil, false, 0, 0},
		{"alternate denied", "denied", "b", "2", meta.InfoHash, nil, false, 1, 0},
		{"alternate unknown", "unknown", "b", "2", meta.InfoHash, nil, false, 1, 0},
		{"alternate same hash", "allowed", "b", "2", meta.InfoHash, nil, true, 1, 0},
		{"alternate same content", "allowed", "b", "2", targetMeta.InfoHash, target, true, 1, 1},
		{"target hash mismatch", "allowed", "b", "2", targetMeta.InfoHash, different, false, 1, 1},
		{"alternate different content", "allowed", "b", "2", unrelatedMeta.InfoHash, different, false, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspections, loads := 0, 0
			err := checkMaintenanceSource(acquirefetch.FetchAcquireResult{
				SiteIdentifier: "a", TorrentID: "1", Meta: meta, TorrentData: donor,
			}, test.site, test.id, test.hash, func(context publishworkflow.Context) publishworkflow.SourceRestriction {
				inspections++
				if context.SiteName != "a" || context.TorrentID != "1" || context.Hash != meta.InfoHash {
					t.Fatalf("wrong donor identity: %+v", context)
				}
				return publishworkflow.SourceRestriction{Status: test.status}
			}, func() ([]byte, error) {
				loads++
				return test.loadData, nil
			})
			if (err == nil) != test.allowed || inspections != test.inspectCalls || loads != test.loadCalls {
				t.Fatalf("err=%v, inspections=%d, loads=%d", err, inspections, loads)
			}
		})
	}
}
