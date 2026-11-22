package media

import (
	"regexp"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
)

var (
	ffprobeInputLine    = regexp.MustCompile(`(?m)^Input #[0-9]+,[^\r\n]+ from '[^\r\n]+':\r?$`)
	ffprobeDurationLine = regexp.MustCompile(`(?m)^[ \t]+Duration: [0-9]+:[0-9]{2}:[0-9]{2}(\.[0-9]+)?,`)
	ffprobeVideoLine    = regexp.MustCompile(`(?m)^[ \t]+Stream #[0-9]+:[0-9]+[^\r\n]*: Video: [^\r\n]+\b[0-9]{2,5}x[0-9]{2,5}\b`)
)

// HasUsableMediaReport 识别 MediaInfo、BDInfo 及下载器端 ffprobe 回退生成的完整视频摘要。
// 参数/返回：输入报告文本，识别成功为 true；空报告和不完整文本为 false。无外部请求和持久化。
func HasUsableMediaReport(text string) bool {
	isMediaInfo, isBDInfo, _ := ValidateMediaInfoFormat(text)
	return isMediaInfo || isBDInfo ||
		(ffprobeInputLine.MatchString(text) && ffprobeDurationLine.MatchString(text) && ffprobeVideoLine.MatchString(text))
}

// QuotedMediaInfo 返回简介中唯一且通过媒体规则的 MediaInfo 报告。
func QuotedMediaInfo(description string) string {
	report := parser.ExtractQuotedMediaInfo(description)
	if isMediaInfo, _, _ := ValidateMediaInfoFormat(report); isMediaInfo {
		return report
	}
	return ""
}
