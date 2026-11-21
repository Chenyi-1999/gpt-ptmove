package media

import "testing"

func TestHasUsableMediaReportKeepsFFprobeFallback(t *testing.T) {
	input := "Input #0, matroska,webm, from '/keep/影片.mkv':\n"
	duration := "  Duration: 01:30:00.25, start: 0.000000, bitrate: 8000 kb/s\n"
	video := "  Stream #0:0(eng): Video: hevc (Main), yuv420p10le, 3840x1736, 25 fps\n"
	if !HasUsableMediaReport(input + duration + video) {
		t.Fatal("ffprobe report with input, duration and video dimensions should be reused")
	}
	for _, incomplete := range []string{"", "尚未生成 MediaInfo", input + duration, input + video, video} {
		if HasUsableMediaReport(incomplete) {
			t.Fatalf("accepted incomplete report: %q", incomplete)
		}
	}
}
