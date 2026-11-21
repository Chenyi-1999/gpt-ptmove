package publishededit

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
)

const (
	maxScreenshotText = 24 << 10
	maxNewImages      = 20
	maxMediaInfoText  = 128 << 10
)

var (
	screenshotTag    = regexp.MustCompile(`(?i)(?:\[url=([^\]\r\n]+)\]\s*)?\[img(?:=[0-9]{1,4}x[0-9]{1,4})?\]([^\[\]\s]+)\[/img\](\s*\[/url\])?`)
	pixhostAssetHost = regexp.MustCompile(`^(?:img|i|t)[0-9]+\.pixhost\.to$`)
)

// Preview 预览追加 Pixhost 截图和更新独立 MediaInfo 字段；nil 表示保留该项。
// 不改动简介原文、原截图或其他表单字段，也不会发起网络请求。
func (snapshot *Snapshot) Preview(screenshotBBCode, mediaInfo *string) (Change, error) {
	change := Change{
		Before: snapshot.Description, After: snapshot.Description,
		MediaInfoBefore: snapshot.MediaInfo, MediaInfoAfter: snapshot.MediaInfo,
		ScreenshotBBCodeBefore: snapshot.ScreenshotBBCode, ScreenshotBBCodeAfter: snapshot.ScreenshotBBCode,
	}
	if screenshotBBCode != nil {
		addition, err := newScreenshots(snapshot.Description, *screenshotBBCode)
		if err != nil {
			return Change{}, err
		}
		if addition != "" {
			if change.After != "" && !strings.HasSuffix(change.After, "\n") {
				change.After += "\n"
			}
			change.After += addition
			change.ScreenshotBBCodeAfter = existingScreenshots(change.After)
		}
	}
	if mediaInfo != nil {
		if !snapshot.MediaInfoEditable {
			return Change{}, fmt.Errorf("%w：NovaHD 表单缺少可编辑的 MediaInfo 字段", ErrUnsupported)
		}
		if len(*mediaInfo) > maxMediaInfoText || strings.ContainsRune(*mediaInfo, 0) {
			return Change{}, fmt.Errorf("%w：MediaInfo 超出限制或包含无效字符", ErrInvalidChange)
		}
		metadata := strings.TrimSpace(normalizeNewlines(*mediaInfo))
		valid, _, _ := processingmedia.ValidateMediaInfoFormat(metadata)
		if !valid {
			return Change{}, fmt.Errorf("%w：请提供完整的 MediaInfo 文本", ErrInvalidChange)
		}
		change.MediaInfoAfter = metadata
	}
	if change.After == change.Before && change.MediaInfoAfter == change.MediaInfoBefore {
		return Change{}, fmt.Errorf("%w：没有新的合法截图或 MediaInfo 变更", ErrInvalidChange)
	}
	return change, nil
}

type screenshot struct {
	assetKey string
	bbcode   string
}

func newScreenshots(description, input string) (string, error) {
	if len(input) > maxScreenshotText {
		return "", fmt.Errorf("%w：截图内容超出长度限制", ErrInvalidChange)
	}
	remaining := strings.TrimSpace(input)
	if remaining == "" {
		return "", fmt.Errorf("%w：截图内容为空", ErrInvalidChange)
	}
	seen := make(map[string]bool)
	for _, shot := range readExistingScreenshots(description) {
		seen[shot.assetKey] = true
	}
	var added []string
	for count := 0; remaining != ""; count++ {
		if count >= maxNewImages {
			return "", fmt.Errorf("%w：每次最多补充 %d 张截图", ErrInvalidChange, maxNewImages)
		}
		var shot screenshot
		match := screenshotTag.FindStringSubmatchIndex(remaining)
		if match != nil && match[0] == 0 {
			shot = parseScreenshot(screenshotTag.FindStringSubmatch(remaining[:match[1]]), true)
			remaining = strings.TrimSpace(remaining[match[1]:])
		} else if strings.HasPrefix(remaining, "https://") {
			end := strings.IndexAny(remaining, " \t\r\n")
			if end == -1 {
				end = len(remaining)
			}
			imageURL := remaining[:end]
			shot = screenshot{assetKey: pixhostKey(imageURL, false, true), bbcode: "[img]" + imageURL + "[/img]"}
			remaining = strings.TrimSpace(remaining[end:])
		}
		if shot.assetKey == "" {
			return "", fmt.Errorf("%w：仅接受有效的 Pixhost HTTPS 图片链接或截图 BBCode", ErrInvalidChange)
		}
		if !seen[shot.assetKey] {
			seen[shot.assetKey] = true
			added = append(added, shot.bbcode)
		}
	}
	return strings.Join(added, "\n"), nil
}

func existingScreenshots(description string) string {
	var items []string
	for _, shot := range readExistingScreenshots(description) {
		items = append(items, shot.bbcode)
	}
	return strings.Join(items, "\n")
}

func readExistingScreenshots(description string) []screenshot {
	var result []screenshot
	for _, match := range screenshotTag.FindAllStringSubmatch(description, -1) {
		if shot := parseScreenshot(match, false); shot.assetKey != "" {
			result = append(result, shot)
		}
	}
	return result
}

func parseScreenshot(match []string, strict bool) screenshot {
	if len(match) != 4 {
		return screenshot{}
	}
	imageURL := match[2]
	assetKey := pixhostKey(imageURL, false, strict)
	if assetKey == "" {
		return screenshot{}
	}
	if (match[1] == "") != (match[3] == "") && strict {
		return screenshot{}
	}
	if match[1] != "" && strict && pixhostKey(strings.Trim(match[1], `"'`), true, true) == "" {
		return screenshot{}
	}
	return screenshot{assetKey: assetKey, bbcode: match[0]}
}

func pixhostKey(raw string, show, strict bool) string {
	if strings.ContainsAny(raw, "[]\\<>'\" \t\r\n\x00") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && (strict || parsed.Scheme != "http")) ||
		parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return ""
	}
	if strict && (parsed.RawQuery != "" || parsed.ForceQuery) {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	assetPath := parsed.Path
	if path.Clean(assetPath) != assetPath || strings.ContainsAny(assetPath, "[]\\<>\r\n\x00") {
		return ""
	}
	if show && host == "pixhost.to" && strings.HasPrefix(assetPath, "/show/") {
		return strings.TrimPrefix(assetPath, "/show/")
	}
	if !show && (host == "pixhost.to" || pixhostAssetHost.MatchString(host)) {
		if strings.HasPrefix(assetPath, "/images/") {
			return strings.TrimPrefix(assetPath, "/images/")
		}
		if strings.HasPrefix(assetPath, "/thumbs/") {
			return strings.TrimPrefix(assetPath, "/thumbs/")
		}
	}
	return ""
}
