package publishededit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/netproxy"
	"golang.org/x/net/html"
)

const (
	novaOrigin       = "https://pt.novahd.top"
	descriptionField = "descr"
	mediaInfoField   = "technical_info"
	maxTrackerHTML   = 4 << 20
)

var (
	ErrUnsupported   = errors.New("NovaHD 种子地址或编辑表单不受支持")
	ErrPermission    = errors.New("无法确认当前 NovaHD 账号拥有该种子及编辑权限")
	ErrConflict      = errors.New("NovaHD 资料已变化，请重新加载并预览")
	ErrInvalidChange = errors.New("补充内容无效")
	ErrUnverified    = errors.New("编辑请求执行结果待核实，请先回读 NovaHD 资料")
	trackerID        = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	novaTransport    = &http.Transport{
		DialContext:           netproxy.ProtectedDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
	}
)

// NovaHD 访问已绑定的 NovaHD 详情和编辑端点，以及独立验证的 Pixhost 封面。
type NovaHD struct {
	client      *http.Client
	coverClient *http.Client
	cookie      string
}

// Snapshot 保存服务端读取的原始资料和用于保存前比较的版本。编辑凭据仅保留在服务端。
type Snapshot struct {
	Description       string `json:"description"`
	Revision          string `json:"revision"`
	ScreenshotBBCode  string `json:"screenshot_bbcode"`
	MediaInfo         string `json:"mediainfo"`
	MediaInfoEditable bool   `json:"mediainfo_editable"`
	CoverURL          string `json:"cover_url"`
	form              editForm
}

// Change 描述页面正文、封面、截图和 MediaInfo 的修改前后值。
type Change struct {
	Before                 string `json:"before"`
	After                  string `json:"after"`
	MediaInfoBefore        string `json:"mediainfo_before"`
	MediaInfoAfter         string `json:"mediainfo_after"`
	ScreenshotBBCodeBefore string `json:"screenshot_bbcode_before"`
	ScreenshotBBCodeAfter  string `json:"screenshot_bbcode_after"`
	CoverURLBefore         string `json:"cover_url_before"`
	CoverURLAfter          string `json:"cover_url_after"`
}

// SaveResult 记录已尝试的变更和回读状态，成功状态以目标站实际值为准。
type SaveResult struct {
	Change   Change `json:"change"`
	Verified bool   `json:"verified"`
}

// NewNovaHD 校验绑定地址并准备带固定超时且禁止跳转的 HTTPS 客户端，不发请求。
func NewNovaHD(baseURL, cookie string) (*NovaHD, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !isNovaURL(base) || (base.Path != "" && base.Path != "/") || base.RawPath != "" || base.RawQuery != "" || base.ForceQuery {
		return nil, ErrUnsupported
	}
	cookie = strings.TrimSpace(cookie)
	if cookie == "" || strings.ContainsAny(cookie, "\r\n") {
		return nil, ErrPermission
	}
	return &NovaHD{
		cookie:      cookie,
		coverClient: pixhostCoverClient,
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: novaTransport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func isNovaURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && u.Opaque == "" && u.Fragment == "" &&
		(strings.EqualFold(u.Host, "pt.novahd.top") || strings.EqualFold(u.Host, "pt.novahd.top:443"))
}

func targetID(publishedURL string) (string, error) {
	detail, err := url.Parse(strings.TrimSpace(publishedURL))
	if err != nil || !isNovaURL(detail) || detail.EscapedPath() != "/details.php" {
		return "", ErrUnsupported
	}
	query, err := url.ParseQuery(detail.RawQuery)
	if err != nil || len(query["id"]) != 1 || !trackerID.MatchString(query.Get("id")) {
		return "", ErrUnsupported
	}
	return query.Get("id"), nil
}

func (n *NovaHD) session() *http.Client {
	client := *n.client
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(novaOrigin)
	request := http.Request{Header: http.Header{"Cookie": {n.cookie}}}
	jar.SetCookies(origin, request.Cookies())
	client.Jar = jar
	return &client
}

// Read 读取详情和编辑表单并核对发种者。只有已确认可编辑时才返回快照。
func (n *NovaHD) Read(ctx context.Context, publishedURL string) (*Snapshot, error) {
	id, err := targetID(publishedURL)
	if err != nil {
		return nil, err
	}
	return readSnapshot(ctx, n.session(), id)
}

func readSnapshot(ctx context.Context, client *http.Client, id string) (*Snapshot, error) {
	detailURL := novaOrigin + "/details.php?id=" + id
	detail, err := getTrackerHTML(ctx, client, detailURL, "")
	if err != nil {
		return nil, err
	}
	page, err := html.Parse(strings.NewReader(detail))
	if err != nil {
		return nil, ErrPermission
	}
	owner, err := verifyOwnership(page, id)
	if err != nil {
		return nil, err
	}
	editURL := novaOrigin + "/edit.php?id=" + id
	content, err := getTrackerHTML(ctx, client, editURL, detailURL)
	if err != nil {
		return nil, err
	}
	form, err := readEditForm(content, editURL, id, owner)
	if err != nil {
		return nil, err
	}
	description, _ := form.value(descriptionField)
	mediaInfo, editable := form.value(mediaInfoField)
	return &Snapshot{
		Description: description, Revision: form.revision(id, owner), ScreenshotBBCode: existingScreenshots(description),
		MediaInfo: mediaInfo, MediaInfoEditable: editable && form.mediaEditable, CoverURL: verifiedNovaCover(page, description), form: form,
	}, nil
}

func getTrackerHTML(ctx context.Context, client *http.Client, endpoint, referer string) (string, error) {
	request, err := trackerRequest(ctx, http.MethodGet, endpoint, referer, nil)
	if err != nil {
		return "", fmt.Errorf("NovaHD 请求无法建立: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("无法连接 NovaHD")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden ||
		(response.StatusCode >= 300 && response.StatusCode < 400) {
		return "", ErrPermission
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("读取 NovaHD 资料失败（HTTP %d）", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxTrackerHTML+1))
	if err != nil || len(payload) > maxTrackerHTML {
		return "", errors.New("NovaHD 资料读取不完整")
	}
	return string(payload), nil
}

func trackerRequest(ctx context.Context, method, endpoint, referer string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 AppleWebKit/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	return request, nil
}

func (form editForm) revision(id, owner string) string {
	fields := make([]formField, 0, len(form.fields))
	for _, field := range form.fields {
		key := strings.ToLower(field.Name)
		if strings.Contains(key, "csrf") || strings.Contains(key, "xsrf") || key == "_token" || key == "authenticity_token" {
			continue
		}
		fields = append(fields, field)
	}
	encoded, _ := json.Marshal([]any{id, owner, form.action, form.enctype, fields})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Save 重新核对所有权和表单版本，预留尝试记录后只提交一次，再打开站点表单核实结果。
// beforeSubmit 失败时不提交。其后的失败统一返回 ErrUnverified，供调用方保留待核状态。
func (n *NovaHD) Save(ctx context.Context, publishedURL, revision string, screenshotBBCode, mediaInfo *string, beforeSubmit func(Change) error) (SaveResult, error) {
	id, err := targetID(publishedURL)
	if err != nil {
		return SaveResult{}, err
	}
	return n.save(ctx, id, revision, func(snapshot *Snapshot) (Change, error) {
		return snapshot.Preview(screenshotBBCode, mediaInfo)
	}, beforeSubmit)
}

// save 使用同一个预览版本、事务审计与回读机制处理各编辑类型。
func (n *NovaHD) save(ctx context.Context, id, revision string, preview func(*Snapshot) (Change, error), beforeSubmit func(Change) error) (SaveResult, error) {
	var result SaveResult
	client := n.session()
	snapshot, err := readSnapshot(ctx, client, id)
	if err != nil {
		return result, err
	}
	if revision != snapshot.Revision {
		return result, ErrConflict
	}
	change, err := preview(snapshot)
	if err != nil {
		return result, err
	}
	result.Change = change
	fields := append([]formField(nil), snapshot.form.fields...)
	for i := range fields {
		switch fields[i].Name {
		case descriptionField:
			fields[i].Value = change.After
		case mediaInfoField:
			if change.CoverURLAfter == "" {
				fields[i].Value = change.MediaInfoAfter
			}
		}
	}
	body, contentType, err := encodeEditForm(fields, snapshot.form.enctype)
	if err != nil {
		return result, err
	}
	request, err := trackerRequest(ctx, http.MethodPost, snapshot.form.action, novaOrigin+"/edit.php?id="+id, body)
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Origin", novaOrigin)
	request.GetBody = nil
	if beforeSubmit == nil {
		return result, fmt.Errorf("%w：缺少编辑尝试记录写入流程", ErrInvalidChange)
	}
	if err := beforeSubmit(change); err != nil {
		return result, err
	}
	response, err := client.Do(request)
	if err == nil {
		response.Body.Close()
	}
	// 不通过重定向或响应正文判定成功。网络错误时也尝试只读核实。
	after, readErr := readSnapshot(ctx, client, id)
	if readErr != nil || after.Description != change.After || after.MediaInfo != change.MediaInfoAfter {
		return result, ErrUnverified
	}
	if change.CoverURLAfter != "" {
		expected := snapshot.form
		expected.fields = fields
		if after.CoverURL != change.CoverURLAfter || after.form.revision(id, "") != expected.revision(id, "") {
			return result, ErrUnverified
		}
	}
	result.Verified = true
	return result, nil
}
