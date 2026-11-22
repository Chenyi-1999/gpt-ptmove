package automation

import (
	"regexp"
	"strconv"
	"strings"

	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"golang.org/x/net/html"
)

type sourcePolicyEvidence struct {
	official   bool
	restricted bool
	episode    bool
	downloaded bool
}

var zmptAccountProgress = regexp.MustCompile(`(?i)^(?:seeding|leeching|inactivity)\s+([0-9]{1,3}(?:\.[0-9]+)?)%$`)

// applySourceEvidence 吸收列表和详情的肯定证据，任何禁转证据都优先于允许结果。
func applySourceEvidence(c *policy.Candidate, evidence sourcePolicyEvidence) {
	c.Official = c.Official || evidence.official
	c.Episode = c.Episode || evidence.episode
	c.PreviouslyDownloaded = c.PreviouslyDownloaded || evidence.downloaded
	if evidence.restricted {
		c.RestrictionsKnown, c.Allowed = true, false
	}
}

// listingSourceEvidence 将织梦账号下载进度与本条标题绑定，避免把他人完成人数视为下载史。
func listingSourceEvidence(row *html.Node, p Profile) sourcePolicyEvidence {
	var evidence sourcePolicyEvidence
	for _, titleCell := range Select(row, "table.torrentname td.embedded") {
		merge := sourceBadgeEvidence(titleCell, p, true)
		evidence.official = evidence.official || merge.official
		evidence.restricted = evidence.restricted || merge.restricted
		evidence.episode = evidence.episode || merge.episode
		evidence.downloaded = evidence.downloaded || merge.downloaded || accountDownloaded(titleCell)
	}
	return evidence
}

// accountDownloaded 识别种子行标题区的账号下载进度，灰色尝试记录也需要排除。
func accountDownloaded(root *html.Node) bool {
	for _, fill := range Select(root, "div[title] > div[style]") {
		match := zmptAccountProgress.FindStringSubmatch(strings.TrimSpace(attr(fill.Parent, "title")))
		if len(match) == 0 || !strings.Contains(strings.ReplaceAll(strings.ToLower(attr(fill, "style")), " ", ""), "width:") {
			continue
		}
		progress, err := strconv.ParseFloat(match[1], 64)
		if err == nil && progress <= 100 {
			return true
		}
	}
	return false
}

// detailSourceEvidence 在详情页保留保守的排除证据，官种只采用实际标签行。
// 织梦的官种证据来自已核对的列表行，详情页的通用导航不用于确认资格。
func detailSourceEvidence(doc *html.Node, p Profile, zmpt bool) sourcePolicyEvidence {
	evidence := sourceBadgeEvidence(doc, p, zmpt)
	evidence.official = false
	if zmpt {
		return evidence
	}
	description := "form, nav, header, #kdescr, #description, .torrent-description, .description"
	if p.Selectors.Description != "" {
		description += ", " + p.Selectors.Description
	}
	excluded := map[*html.Node]bool{}
	for _, n := range Select(doc, description) {
		excluded[n] = true
	}
	for _, label := range Select(doc, "tr > td.rowhead, tr > th.rowhead, tr > th") {
		name := strings.ToLower(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(text(label)), "：:")))
		if name != "标签" && name != "標籤" && name != "tags" && name != "tag" {
			continue
		}
		untrusted := false
		for parent := label; parent != nil; parent = parent.Parent {
			if excluded[parent] {
				untrusted = true
				break
			}
		}
		if untrusted {
			continue
		}
		for value := label.NextSibling; value != nil; value = value.NextSibling {
			if value.Type != html.ElementNode {
				continue
			}
			if (value.Data == "td" || value.Data == "th") && (sourceBadgeEvidence(value, p, false).official || sourceBadgeEvidence(value, p, true).official) {
				evidence.official = true
				return evidence
			}
			break
		}
	}
	return evidence
}

// sourceBadgeEvidence 只接受明确的标签名。织梦官方身份须由该条标签证实。
func sourceBadgeEvidence(root *html.Node, p Profile, zmpt bool) sourcePolicyEvidence {
	var evidence sourcePolicyEvidence
	selectors := `a[href*="tag_id="], .tag, .torrent-tag`
	if zmpt {
		selectors += ", span[title][style]"
	} else if p.Selectors.Tags != "" {
		selectors += ", " + p.Selectors.Tags
	}
	for _, tag := range Select(root, selectors) {
		if zmpt && !zmptTagBadge(tag) {
			continue
		}
		for _, value := range []string{text(tag), attr(tag, "title"), attr(tag, "aria-label"), attr(tag, "alt")} {
			switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "tag.") {
			case "官方", "官方种", "官方種", "官种", "官種", "official":
				evidence.official = true
			case "禁转", "禁轉", "限转", "限轉", "禁止转载", "禁止轉載":
				evidence.restricted = true
			case "分集", "单集", "單集":
				evidence.episode = true
			case "已下载", "已下載", "下载过", "下載過":
				evidence.downloaded = true
			}
		}
	}
	return evidence
}

// zmptTagBadge 匹配织梦原生标签的无 class 彩色 span，排除普通标题和导航链接。
func zmptTagBadge(n *html.Node) bool {
	style := strings.ReplaceAll(strings.ToLower(attr(n, "style")), " ", "")
	return n.Data == "span" && strings.Contains(style, "background-color:") && strings.Contains(style, "border-radius:")
}
