package automation

import (
	"testing"

	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

type fakeSeeding struct {
	status    downloaderclient.AutomationTorrentStatus
	complete  bool
	trackers  []downloaderclient.AutomationTracker
	fileCalls int
}

func (f *fakeSeeding) TorrentStatus(string) (downloaderclient.AutomationTorrentStatus, error) {
	return f.status, nil
}
func (f *fakeSeeding) TorrentFilesComplete(string) (bool, error) {
	f.fileCalls++
	return f.complete, nil
}
func (f *fakeSeeding) Trackers(string) ([]downloaderclient.AutomationTracker, error) {
	return f.trackers, nil
}
func TestSeedingRequiresWholeFilesAndTargetTracker(t *testing.T) {
	f := &fakeSeeding{status: downloaderclient.AutomationTorrentStatus{Found: true, Progress: 1, AmountLeft: 0, State: "stalledUP"}, complete: false, trackers: []downloaderclient.AutomationTracker{{URL: "https://announce.target.example.com/announce?passkey=not-logged", Status: 2}}}
	if ok, err := VerifySeeding(f, "hash", []string{"announce.target.example.com"}); err != nil || ok {
		t.Fatalf("partial files accepted: %v %v", ok, err)
	}
	f.complete = true
	if ok, err := VerifySeeding(f, "hash", []string{"announce.target.example.com"}); err != nil || !ok {
		t.Fatalf("complete target not accepted: %v %v", ok, err)
	}
	if ok, _ := VerifySeeding(f, "hash", []string{"announce.source.example.com"}); ok {
		t.Fatal("source tracker mistaken for target")
	}
	f.trackers[0].Status = 4
	if ok, _ := VerifySeeding(f, "hash", []string{"announce.target.example.com"}); ok {
		t.Fatal("offline target tracker accepted")
	}
	f.status.Progress = .99
	if ok, _ := VerifySeeding(f, "hash", []string{"announce.target.example.com"}); ok {
		t.Fatal("partial overall payload accepted")
	}
}
func TestPausedTorrentIsNotReportedAsSeeding(t *testing.T) {
	f := &fakeSeeding{status: downloaderclient.AutomationTorrentStatus{Found: true, Progress: 1, AmountLeft: 0, State: "pausedUP"}, complete: true}
	if ok, _ := VerifySeeding(f, "hash", []string{"target.example.com"}); ok {
		t.Fatal("paused torrent reported seeding")
	}
	if f.fileCalls != 0 {
		t.Fatal("paused torrent advanced to file verification")
	}
	f.status.State = "queuedUP"
	if ok, _ := VerifySeeding(f, "hash", []string{"target.example.com"}); ok {
		t.Fatal("queued upload was reported actively seeding")
	}
}
