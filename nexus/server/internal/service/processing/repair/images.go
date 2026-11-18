package repair

import (
	neturl "net/url"
	"regexp"
	"strings"

	"github.com/pt-nexus/server/internal/service/acquire/extract"
)

var reImgBBCode = regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`)

// ToBBCodeImages 将图片 URL 列表转换为 BBCode（每行一个 [img]...[/img]）。
func ToBBCodeImages(urls []string) string {
	clean := make([]string, 0, len(urls))
	for _, raw := range urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		clean = appendUniqueStringLocal(clean, url)
	}
	if len(clean) == 0 {
		return ""
	}
	rows := make([]string, 0, len(clean))
	for _, url := range clean {
		rows = append(rows, "[img]"+url+"[/img]")
	}
	return strings.Join(rows, "\n")
}

// ExtractImageURLsFromText 从 BBCode 或纯文本中提取图片 URL（去重）。
func ExtractImageURLsFromText(text string) []string {
	return extract.ExtractImageURLsFromText(text)
}

// AbsolutizeURL 以 baseURL 为基准将相对链接补全为绝对链接。
func AbsolutizeURL(baseURL string, raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	parsed, err := neturl.Parse(value)
	if err == nil && parsed.IsAbs() {
		return value
	}
	if strings.TrimSpace(baseURL) == "" {
		return value
	}
	base, err := neturl.Parse(baseURL)
	if err != nil {
		return value
	}
	resolved := base.ResolveReference(parsed)
	if resolved == nil {
		return value
	}
	return resolved.String()
}

func appendUniqueStringLocal(items []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return items
	}
	for _, existing := range items {
		if existing == trimmed {
			return items
		}
	}
	return append(items, trimmed)
}
