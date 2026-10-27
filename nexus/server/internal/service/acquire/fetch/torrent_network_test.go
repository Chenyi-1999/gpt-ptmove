package fetch

import (
	"fmt"
	"strings"
	"testing"
)

func privateFixture(tracker, extra string) []byte {
	info := "d6:lengthi1e4:name4:test12:piece lengthi1e6:pieces20:012345678901234567897:privatei1e" + extra + "e"
	return []byte(fmt.Sprintf("d8:announce%d:%s4:info%se", len(tracker), tracker, info))
}
func TestTorrentNetworkAndPayloadValidation(t *testing.T) {
	valid := privateFixture("https://tracker.example/announce", "")
	if ValidateTorrentNetwork(valid, "tracker.example") != nil {
		t.Fatal("valid private fixture rejected")
	}
	if ValidateTorrentNetwork(valid, "other.example") == nil {
		t.Fatal("undeclared tracker accepted")
	}
	for _, tracker := range []string{"https://81.70.24.5/announce", "https://[::ffff:81.70.24.5]/announce", "udp://tracker.example/announce"} {
		if ValidateTorrentNetwork(privateFixture(tracker, "")) == nil {
			t.Fatal("forbidden tracker accepted")
		}
	}
	withSource := privateFixture("https://other.example/announce", "6:source1:B")
	if CompareTorrentPayload(valid, withSource) != nil {
		t.Fatal("site source field changed payload equality")
	}
	changed := []byte(strings.Replace(string(valid), "20:01234567890123456789", "20:11234567890123456789", 1))
	if CompareTorrentPayload(valid, changed) == nil {
		t.Fatal("different pieces accepted")
	}
	for _, raw := range [][]byte{append(valid, 'e'), []byte("d999999999999999999:"), []byte("d9223372036854775807:x"), privateFixture("https://tracker.example/announce", "10:name.utf-82:.."), privateFixture("https://tracker.example/announce", "6:lengthi1e")} {
		if _, err := TorrentNetworkURLs(raw); err == nil {
			t.Fatal("malformed or unsafe torrent accepted")
		}
	}
}
