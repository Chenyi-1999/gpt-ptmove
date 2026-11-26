package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type downloader struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Uploaded   int64  `json:"uploaded_bytes"`
	Downloaded int64  `json:"downloaded_bytes"`
}

type request struct {
	Action   string `json:"action"`
	Settings struct {
		IncludeDisabled bool `json:"include-disabled"`
	} `json:"settings"`
	Resources struct {
		Downloaders []downloader `json:"downloader.summary"`
	} `json:"resources"`
}

type block struct {
	Kind    string     `json:"kind"`
	Title   string     `json:"title"`
	Text    string     `json:"text,omitempty"`
	Value   string     `json:"value,omitempty"`
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
}

func main() {
	var input request
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20)).Decode(&input); err != nil || input.Action != "summary" {
		os.Exit(1)
	}
	var rows [][]string
	for _, item := range input.Resources.Downloaders {
		if !input.Settings.IncludeDisabled && !item.Enabled {
			continue
		}
		state := "已启用"
		if !item.Enabled {
			state = "已停用"
		}
		rows = append(rows, []string{item.Name, state, gib(item.Uploaded), gib(item.Downloaded)})
	}
	blocks := []block{{Kind: "metric", Title: "显示的下载器", Value: fmt.Sprint(len(rows))}}
	if len(rows) == 0 {
		blocks = append(blocks, block{Kind: "text", Title: "暂无数据", Text: "当前设置下没有可显示的下载器。"})
	} else {
		blocks = append(blocks, block{Kind: "table", Title: "已记录的流量", Columns: []string{"下载器", "状态", "累计上传", "累计下载"}, Rows: rows})
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"message": "统计完成", "blocks": blocks}); err != nil {
		os.Exit(1)
	}
}

func gib(bytes int64) string {
	return fmt.Sprintf("%.2f GiB", float64(bytes)/(1<<30))
}
