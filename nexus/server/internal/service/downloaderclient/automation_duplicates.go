package downloaderclient

import (
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var (
	automationSourceTaskID = regexp.MustCompile(`^auto-[a-z0-9]{1,64}$`)
	automationSourceHash   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ErrAutomationDuplicateCleanupPending 表示 qB 已接受请求但还未确认最终状态，需要查阅下载器。
var ErrAutomationDuplicateCleanupPending = errors.New("qB 已接受重复种子处理请求，结果仍待核对")

type automationDuplicateTorrent struct {
	Hash        string   `json:"hash"`
	SavePath    string   `json:"save_path"`
	ContentPath string   `json:"content_path"`
	State       string   `json:"state"`
	Tags        string   `json:"tags"`
	Progress    *float64 `json:"progress"`
	AmountLeft  *int64   `json:"amount_left"`
	AutoManaged *bool    `json:"auto_tmm"`
}

func automationSourceOwnershipTag(taskID string) string {
	if !automationSourceTaskID.MatchString(taskID) {
		return ""
	}
	return "starpt-" + taskID
}

// HandleAutomationDuplicate 在写入授权仍有效时处理一个确认重复的自动化源任务。
// 调用方应已确认 IYUU 命中、持有任务租约并记录此请求不会被盲目重试。
// qB 必须返回该哈希及由 STARPT 添加的任务标签。delete 仅删除种子记录，move 使用 qB 异步搬移数据。
func (d Downloader) HandleAutomationDuplicate(hash, taskID, expectedSavePath, action, location string) error {
	if action == "keep" {
		return nil
	}
	tag := automationSourceOwnershipTag(taskID)
	hash = strings.ToLower(hash)
	origin := automationDuplicatePath(expectedSavePath)
	if d.effectGuard == nil || tag == "" || !automationSourceHash.MatchString(hash) || origin == "" {
		return errors.New("缺少已授权的自动化任务、哈希或下载目录")
	}
	endpoint := "torrents/delete"
	params := url.Values{"hashes": {hash}}
	switch action {
	case "move":
		if automationDuplicatePath(location) == origin {
			return errors.New("重复资源移动目录必须是与原目录不同的绝对路径")
		}
		location = automationDuplicateDestination(location, taskID)
		if location == "" {
			return errors.New("重复资源移动目录无效")
		}
		endpoint = "torrents/setLocation"
		params.Set("location", location)
	case "delete":
		params.Set("deleteFiles", "false")
	default:
		return errors.New("重复种子处理方式无效")
	}
	c, err := d.automationQB()
	if err != nil {
		return err
	}
	rows, err := automationDuplicateRows(c, hash)
	if err != nil {
		return err
	}
	if automationDuplicateComplete(rows, tag, action, location) {
		return nil
	}
	if len(rows) != 1 || !rows[0].ownedBy(tag) {
		return errors.New("qB 未确认该种子属于此自动化任务")
	}
	row := rows[0]
	saved := automationDuplicatePath(row.SavePath)
	if saved != origin || !row.readyForCleanup() {
		return errors.New("qB 未确认源任务已完整下载，或保存路径已变化")
	}
	if action == "move" {
		if err = verifyAutomationDuplicateMove(c, row, origin, location, tag); err != nil {
			return err
		}
	}
	body, postErr := c.PostForm(endpoint, params)
	rows, err = automationDuplicateRows(c, hash)
	if err == nil && automationDuplicateComplete(rows, tag, action, location) {
		return nil
	}
	if postErr == nil {
		switch strings.TrimSpace(string(body)) {
		case "", "Ok", "Ok.":
			return ErrAutomationDuplicateCleanupPending
		}
	}
	return errors.New("qB 重复种子处理结果未知，请在下载器核对")
}

// AutomationDuplicateCleanupComplete 只读检查已提交的重复种子处理是否完成，适用于移动和删除回执待核对。
func (d Downloader) AutomationDuplicateCleanupComplete(hash, taskID, action, location string) (bool, error) {
	tag := automationSourceOwnershipTag(taskID)
	hash = strings.ToLower(hash)
	if tag == "" || !automationSourceHash.MatchString(hash) {
		return false, errors.New("重复种子处理任务或哈希无效")
	}
	switch action {
	case "move":
		location = automationDuplicateDestination(location, taskID)
		if location == "" {
			return false, errors.New("重复资源移动目录无效")
		}
	case "delete":
	default:
		return false, errors.New("重复种子处理方式无效")
	}
	c, err := d.automationQB()
	if err != nil {
		return false, err
	}
	rows, err := automationDuplicateRows(c, hash)
	if err != nil {
		return false, err
	}
	if len(rows) == 1 && !rows[0].ownedBy(tag) {
		return false, errors.New("qB 未确认当前种子属于此自动化任务")
	}
	return automationDuplicateComplete(rows, tag, action, location), nil
}

func automationDuplicateComplete(rows []automationDuplicateTorrent, tag, action, location string) bool {
	if action == "delete" {
		return len(rows) == 0
	}
	if len(rows) != 1 || !rows[0].ownedBy(tag) || !rows[0].readyForCleanup() || automationDuplicatePath(rows[0].SavePath) != location {
		return false
	}
	content := automationDuplicatePath(rows[0].ContentPath)
	return content != "" && content != location && automationDuplicateWithin(content, location)
}

// automationDuplicateRows 读取目标哈希或完整清单。格式或身份不明确的响应不能作为清理依据。
func automationDuplicateRows(c *qbClient, hash string) ([]automationDuplicateTorrent, error) {
	var query url.Values
	if hash != "" {
		query = url.Values{"hashes": {hash}}
	}
	body, err := c.Get("torrents/info", query)
	if err != nil {
		return nil, errors.New("无法读取 qB 种子清单以核对重复处理")
	}
	var rows []automationDuplicateTorrent
	if json.Unmarshal(body, &rows) != nil || rows == nil || (hash != "" && (len(rows) > 1 || (len(rows) == 1 && !strings.EqualFold(rows[0].Hash, hash)))) {
		return nil, errors.New("qB 种子清单格式或种子哈希无效")
	}
	return rows, nil
}

func (row automationDuplicateTorrent) ownedBy(tag string) bool {
	for _, value := range strings.Split(row.Tags, ",") {
		if strings.TrimSpace(value) == tag {
			return true
		}
	}
	return false
}

func (row automationDuplicateTorrent) readyForCleanup() bool {
	switch row.State {
	case "", "moving", "checkingUP", "checkingDL", "checkingResumeData", "error", "missingFiles", "unknown", "allocating":
		return false
	}
	return row.Progress != nil && *row.Progress == 1 && row.AmountLeft != nil && *row.AmountLeft == 0
}

// verifyAutomationDuplicateMove 使用 qB 清单防止移动其他种子正在使用或占用目标路径的内容。
func verifyAutomationDuplicateMove(c *qbClient, row automationDuplicateTorrent, origin, location, tag string) error {
	content := automationDuplicatePath(row.ContentPath)
	if row.AutoManaged == nil || *row.AutoManaged || content == "" || !automationDuplicateWithin(content, origin) || content == origin || automationDuplicateWithin(location, content) {
		return errors.New("qB 未确认源种子可从原位置安全移动")
	}
	all, err := automationDuplicateRows(c, "")
	if err != nil {
		return err
	}
	sourcePresent := false
	for _, other := range all {
		if strings.EqualFold(other.Hash, row.Hash) {
			if sourcePresent || !other.ownedBy(tag) || automationDuplicatePath(other.SavePath) != origin || automationDuplicatePath(other.ContentPath) != content || other.AutoManaged == nil || *other.AutoManaged || !other.readyForCleanup() {
				return errors.New("qB 源种子已变化，取消移动")
			}
			sourcePresent = true
			continue
		}
		if !automationSourceHash.MatchString(strings.ToLower(other.Hash)) {
			return errors.New("qB 清单存在身份未知的种子，取消移动")
		}
		otherPath := automationDuplicatePath(other.ContentPath)
		if otherPath == "" {
			otherPath = automationDuplicatePath(other.SavePath)
		}
		if otherPath == "" || automationDuplicateOverlap(otherPath, content) || automationDuplicateOverlap(otherPath, location) {
			return errors.New("源内容或目标位置可能与其他 qB 种子共用，取消移动")
		}
	}
	if !sourcePresent {
		return errors.New("qB 清单中缺少源种子，取消移动")
	}
	return nil
}

func automationDuplicateDestination(location, taskID string) string {
	base := automationDuplicatePath(location)
	if base == "" || base == "/" {
		return ""
	}
	return path.Join(base, taskID)
}

func automationDuplicatePath(value string) string {
	if !path.IsAbs(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\:\x00\r\n") {
		return ""
	}
	if value == "/" {
		return value
	}
	value = strings.TrimRight(value, "/")
	if path.Clean(value) != value {
		return ""
	}
	return value
}

func automationDuplicateWithin(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, strings.TrimRight(parent, "/")+"/")
}

func automationDuplicateOverlap(left, right string) bool {
	return automationDuplicateWithin(left, right) || automationDuplicateWithin(right, left)
}
