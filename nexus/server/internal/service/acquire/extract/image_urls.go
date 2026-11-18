package extract

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var (
	imageBBCode = regexp.MustCompile(`(?is)\[img(?:=[^\]]*)?\](.*?)\[/img\]`)
	linkBBCode  = regexp.MustCompile(`(?is)\[url=[^\]]*\](.*?)\[/url\]`)
	bareImage   = regexp.MustCompile(`(?i)https?://[^\s\[\]"'<>]+\.(?:jpg|jpeg|png|webp|gif)(?:\?[^\s\[\]"'<>#]*)?(?:#[^\s\[\]"'<>]*)?`)
)

// ExtractImageURLsFromText 提取 BBCode 图片与裸图地址，维持原有的图片标签优先顺序。
// 同一 HTTP 资源仅保留首次出现的地址。旧草稿中的 Pixhost 展示页有对应原图时会被移除。
func ExtractImageURLsFromText(text string) []string {
	images := []string{}
	seen := map[string]bool{}
	direct := map[string]bool{}
	showPages := map[string]string{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		key, _, _ := strings.Cut(raw, "#")
		if key == "" || seen[key] {
			return
		}
		parsed, err := url.Parse(raw)
		if err == nil {
			host := strings.ToLower(parsed.Hostname())
			if host == "pixhost.to" || strings.HasSuffix(host, ".pixhost.to") {
				kind, resource, _ := strings.Cut(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
				if resource != "" {
					switch kind {
					case "images":
						// Pixhost 用路径标识同一张原图，查询参数和 CDN 域名可能不同。
						if direct[resource] {
							return
						}
						direct[resource] = true
					case "show":
						showPages[raw] = resource
					}
				}
			}
		}
		seen[key] = true
		images = append(images, raw)
	}

	for _, match := range imageBBCode.FindAllStringSubmatch(text, -1) {
		add(match[1])
	}
	// 保留普通超链接。带图片的链接只扫描正文，避免把 href 当成第二张图。
	plain := linkBBCode.ReplaceAllStringFunc(text, func(block string) string {
		if !imageBBCode.MatchString(block) {
			return block
		}
		_, body, _ := strings.Cut(block, "]")
		return body
	})
	plain = imageBBCode.ReplaceAllString(plain, " ")
	for _, raw := range bareImage.FindAllString(plain, -1) {
		add(raw)
	}
	return slices.DeleteFunc(images, func(raw string) bool {
		return direct[showPages[raw]]
	})
}
