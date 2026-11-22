package downloaderclient

import (
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"strings"
)

type automationDownloadedFile struct {
	Name     string   `json:"name"`
	Size     *int64   `json:"size"`
	Progress *float64 `json:"progress"`
	Priority *int     `json:"priority"`
}

// CompletedMediaPath 读取指定种子的 qB 文件列表，确认全部文件完整，再返回该种子内的主要视频单文件路径。
// 下载器文件名不安全、文件未下载完整或无法在冻结的内容路径下定位视频时拒绝，避免扫描保存目录。
func (d Downloader) CompletedMediaPath(hash, savePath, contentPath string) (string, error) {
	c, err := d.automationQB()
	if err != nil {
		return "", err
	}
	body, err := c.Get("torrents/files", url.Values{"hash": {hash}})
	if err != nil {
		return "", errors.New("读取下载器媒体文件列表失败")
	}
	var files []automationDownloadedFile
	if json.Unmarshal(body, &files) != nil || len(files) == 0 {
		return "", errors.New("下载器媒体文件列表未知")
	}
	return completedMediaPath(files, savePath, contentPath)
}

func completedMediaPath(files []automationDownloadedFile, savePath, contentPath string) (string, error) {
	savePath, contentPath = path.Clean(savePath), path.Clean(contentPath)
	if !path.IsAbs(savePath) || !strings.HasPrefix(contentPath, strings.TrimRight(savePath, "/")+"/") || contentPath == savePath {
		return "", errors.New("媒体内容路径不在种子的保存目录中")
	}
	var bestPath string
	var bestSize int64
	for _, file := range files {
		if file.Progress == nil || *file.Progress != 1 || file.Priority == nil || *file.Priority <= 0 || file.Size == nil || *file.Size < 0 {
			return "", errors.New("下载器未确认种子的全部文件已完整下载")
		}
		rel := file.Name
		if rel == "" || path.IsAbs(rel) || strings.ContainsAny(rel, "\\:\x00\r\n") || path.Clean(rel) != rel || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", errors.New("下载器返回了无效的种子内文件路径")
		}
		name := path.Join(savePath, rel)
		if name != contentPath && !strings.HasPrefix(name, contentPath+"/") {
			return "", errors.New("下载器的文件列表与种子内容路径不一致")
		}
		switch strings.ToLower(path.Ext(name)) {
		case ".mkv", ".mp4", ".m4v", ".ts", ".m2ts", ".avi", ".mov", ".mpg", ".mpeg", ".wmv", ".webm", ".flv":
			if *file.Size > bestSize || (*file.Size > 0 && *file.Size == bestSize && name < bestPath) {
				bestPath, bestSize = name, *file.Size
			}
		}
	}
	if bestPath == "" {
		return "", errors.New("未找到属于此种子的已完成视频文件")
	}
	return bestPath, nil
}
