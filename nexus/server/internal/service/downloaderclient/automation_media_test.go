package downloaderclient

import (
	"encoding/json"
	"testing"
)

// TestCompletedMediaPathBounds 验证只从完整的同一种子清单取视频文件。
func TestCompletedMediaPathBounds(t *testing.T) {
	for _, tc := range []struct {
		name, files, content, want string
	}{
		{"single", `[{"name":"Movie.mkv","size":100,"priority":1,"progress":1}]`, "/nas/video/Movie.mkv", "/nas/video/Movie.mkv"},
		{"largest", `[{"name":"Series/E01.mkv","size":80,"priority":1,"progress":1},{"name":"Series/E02.mkv","size":100,"priority":1,"progress":1},{"name":"Series/data.txt","size":500,"priority":1,"progress":1}]`, "/nas/video/Series", "/nas/video/Series/E02.mkv"},
		{"escape", `[{"name":"../other/Movie.mkv","size":100,"priority":1,"progress":1}]`, "/nas/video/Movie.mkv", ""},
		{"outside torrent", `[{"name":"Other/Movie.mkv","size":100,"priority":1,"progress":1}]`, "/nas/video/Movie", ""},
		{"skip file", `[{"name":"Series/E01.mkv","size":100,"priority":1,"progress":1},{"name":"Series/E02.mkv","size":80,"priority":0,"progress":1}]`, "/nas/video/Series", ""},
		{"incomplete", `[{"name":"Movie.mkv","size":100,"priority":1,"progress":0.99}]`, "/nas/video/Movie.mkv", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var files []automationDownloadedFile
			if err := json.Unmarshal([]byte(tc.files), &files); err != nil {
				t.Fatal(err)
			}
			got, err := completedMediaPath(files, "/nas/video", tc.content)
			if got != tc.want || (tc.want == "") != (err != nil) {
				t.Fatalf("path=%q error=%v wanted %q", got, err, tc.want)
			}
		})
	}
}
