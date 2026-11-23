package media

import (
	"errors"
	"testing"
)

type missingReportRepo struct {
	row    map[string]any
	reads  int
	writes int
}

func (r *missingReportRepo) GetSeedParameterByKey(_, _, _ string) (map[string]any, error) {
	r.reads++
	if r.row == nil {
		return nil, errors.New("draft missing")
	}
	return r.row, nil
}

func (r *missingReportRepo) UpdateSeedParameterByKey(_, _, _ string, _ map[string]any) error {
	r.writes++
	return nil
}

func TestFillMissingMediaInfoKeepsDraftCacheAndSupportsProxyExtraction(t *testing.T) {
	const report = "Input #0, matroska,webm, from '/home/fixture.mkv':\n" +
		"  Duration: 01:30:00.25, start: 0.000000, bitrate: 8000 kb/s\n" +
		"  Stream #0:0(eng): Video: hevc (Main), yuv420p10le, 3840x1736, 25 fps\n"
	for _, tt := range []struct {
		name      string
		row       map[string]any
		proxy     ProxyMediaInfoProbe
		code      int
		wantProbe bool
	}{
		{name: "cached", row: map[string]any{"mediainfo": report}, code: 200},
		{name: "new remote report", row: map[string]any{}, proxy: ProxyMediaInfoProbe{StatusCode: 200, Success: true, MediaInfo: report}, code: 200, wantProbe: true},
		{name: "incomplete report", row: map[string]any{}, proxy: ProxyMediaInfoProbe{StatusCode: 200, Success: true, MediaInfo: "Incomplete"}, code: 422, wantProbe: true},
		{name: "Blu-ray", row: map[string]any{}, proxy: ProxyMediaInfoProbe{StatusCode: 200, Success: true, IsBDMV: true}, code: 422, wantProbe: true},
		{name: "existing BDInfo job", row: map[string]any{"mediainfo_status": "processing_bdinfo"}, code: 409},
		{name: "missing draft", code: 404},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &missingReportRepo{row: tt.row}
			probed := false
			result, code := RefreshMediainfoAsync(map[string]any{
				"seed_id": "draft", "save_path": "/home", "torrent_name": "fixture.mkv", "downloader_id": "client", "fill_missing": true,
			}, repo, RefreshMediainfoDeps{
				ParseSeedID: func(string) (string, string, string, error) { return "hash", "id", "site", nil },
				FetchProxyMediaInfo: func(_, _, _ string) ProxyMediaInfoProbe {
					probed = true
					return tt.proxy
				},
				StartBDInfoTask: func(string, bool) (string, error) {
					t.Fatal("fill_missing must not start a DB-writing BDInfo task")
					return "", nil
				},
				AfterPersist: func(_, _, _ string, _ map[string]any, _, _, _ string) { t.Fatal("unexpected postprocessing") },
			})
			if code != tt.code || probed != tt.wantProbe || repo.writes != 0 || repo.reads != 1 {
				t.Fatalf("status=%d probe=%v reads=%d writes=%d result=%v", code, probed, repo.reads, repo.writes, result)
			}
			if code == 200 && !HasUsableMediaReport(result["mediainfo"].(string)) {
				t.Fatal("expected a usable report")
			}
		})
	}
}
