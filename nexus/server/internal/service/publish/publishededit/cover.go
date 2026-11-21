package publishededit

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"golang.org/x/net/html"
)

var (
	// ErrCoverMissing 表示详情页没有与简介首张图片一致的封面。
	ErrCoverMissing = errors.New("无法确认 NovaHD 当前封面")
	// ErrCoverUnchanged 表示解析出的原图已是当前封面。
	ErrCoverUnchanged = errors.New("已在使用这张封面")
	// ErrCoverUnavailable 表示未找到当前能打开的 Pixhost 原图。
	ErrCoverUnavailable = errors.New("未找到能打开的 Pixhost 原图")
)

type coverImageSpan struct {
	imageStart, imageEnd int
	linkStart, linkEnd   int
	wrapperEnd           int
	end                  int
}

func leadingCoverImage(description string) (coverImageSpan, bool) {
	match := screenshotTag.FindStringSubmatchIndex(description)
	if match == nil {
		return coverImageSpan{}, false
	}
	return coverImageSpan{imageStart: match[4], imageEnd: match[5], linkStart: match[2], linkEnd: match[3], wrapperEnd: match[7], end: match[1]}, true
}

// verifiedNovaCover 只把与简介首张图片一致的唯一页面头图识别为封面。
func verifiedNovaCover(page *html.Node, description string) string {
	leading, ok := leadingCoverImage(description)
	if !ok {
		return ""
	}
	image := description[leading.imageStart:leading.imageEnd]
	u, err := url.Parse(image)
	if err != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	count := 0
	for node := range page.Descendants() {
		if node.Type == html.ElementNode && node.Data == "img" && hasClass(node, "nova-movie-hero-poster") {
			if attr(node, "src") != image {
				return ""
			}
			count++
		}
	}
	if count != 1 {
		return ""
	}
	return image
}

// PreviewCover 根据已核实的头图位置生成新旧差异，不访问站点或写入数据。
func (snapshot *Snapshot) PreviewCover(coverURL string) (Change, error) {
	address, ok := parsePixhostCoverAddress(coverURL)
	if !ok || address.kind != "images" {
		return Change{}, fmt.Errorf("%w：封面需要 Pixhost 原图直链", ErrInvalidChange)
	}
	coverURL = address.url.String()
	leading, ok := leadingCoverImage(snapshot.Description)
	if !ok || snapshot.CoverURL == "" || snapshot.Description[leading.imageStart:leading.imageEnd] != snapshot.CoverURL {
		return Change{}, ErrCoverMissing
	}
	if coverURL == snapshot.CoverURL {
		return Change{}, ErrCoverUnchanged
	}
	description := snapshot.Description
	prefix := description[:leading.imageStart]
	if leading.linkStart >= 0 && leading.wrapperEnd >= 0 {
		link := description[leading.linkStart:leading.linkEnd]
		replacement, ok := coverAssetLink(link, snapshot.CoverURL, address)
		if ok {
			prefix = description[:leading.linkStart] + replacement + description[leading.linkEnd:leading.imageStart]
		}
	}
	screenshots := existingScreenshots(description[leading.end:])
	return Change{
		Before: description, After: prefix + coverURL + description[leading.imageEnd:],
		MediaInfoBefore: snapshot.MediaInfo, MediaInfoAfter: snapshot.MediaInfo,
		ScreenshotBBCodeBefore: screenshots, ScreenshotBBCodeAfter: screenshots,
		CoverURLBefore: snapshot.CoverURL, CoverURLAfter: coverURL,
	}, nil
}

// coverAssetLink 仅替换确实指向原封面资源的外层链接，其他链接全文保留。
func coverAssetLink(link, oldCover string, replacement pixhostCoverAddress) (string, bool) {
	quote := ""
	if len(link) >= 2 && (link[0] == '"' || link[0] == '\'') && link[0] == link[len(link)-1] {
		quote = link[:1]
		link = link[1 : len(link)-1]
	}
	prev, ok := parsePixhostCoverAddress(oldCover)
	if !ok {
		return "", false
	}
	target, ok := parsePixhostCoverAddress(link)
	if !ok || prev.group != target.group || prev.name != target.name {
		return "", false
	}
	// 同一张图换服务器时保留原展示页；换图后使用已实际核验的新直链，避免猜测展示页文件名。
	if target.kind == "show" && target.group == replacement.group && target.name == replacement.name {
		return quote + link + quote, true
	}
	return quote + replacement.url.String() + quote, true
}

// SaveCover 核对预览版本和原图，提交一次封面变更，再回读头图及表单核实。
func (n *NovaHD) SaveCover(ctx context.Context, publishedURL, revision, coverURL string, beforeSubmit func(Change) error) (SaveResult, error) {
	id, err := targetID(publishedURL)
	if err != nil {
		return SaveResult{}, err
	}
	address, ok := parsePixhostCoverAddress(coverURL)
	if !ok || address.kind != "images" {
		return SaveResult{}, fmt.Errorf("%w：封面需要 Pixhost 原图直链", ErrInvalidChange)
	}
	// 在读取最新 NovaHD 表单前验证直链，避免图床请求延长快照到提交的间隔。
	if !pixhostOriginalAvailable(ctx, n.coverClient, address) {
		return SaveResult{}, ErrCoverUnavailable
	}
	return n.save(ctx, id, revision, func(snapshot *Snapshot) (Change, error) {
		return snapshot.PreviewCover(coverURL)
	}, beforeSubmit)
}
