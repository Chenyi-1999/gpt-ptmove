package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"unicode/utf8"
)

const DownloaderSummaryCapability = "downloader.summary"

var (
	ErrPackageNotFound  = errors.New("插件不存在")
	ErrPackageDisabled  = errors.New("插件已停用")
	ErrPackageAction    = errors.New("插件未声明此操作")
	ErrPackageSettings  = errors.New("插件配置无效")
	ErrPackageExecution = errors.New("插件执行失败")
)

type Field struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Default any      `json:"default"`
	Options []string `json:"options,omitempty"`
}

type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type PackageManifest struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Description   string   `json:"description,omitempty"`
	Fields        []Field  `json:"fields"`
	Actions       []Action `json:"actions"`
	Capabilities  []string `json:"capabilities"`
}

type PackageInfo struct {
	PackageManifest
	Kind     string         `json:"kind"`
	Enabled  bool           `json:"enabled"`
	Settings map[string]any `json:"settings"`
}

type PackageSnapshot struct {
	Items  []PackageInfo `json:"items"`
	Issues []Issue       `json:"issues"`
}

type ActionResult struct {
	Message string        `json:"message,omitempty"`
	Blocks  []ResultBlock `json:"blocks"`
}

type ResultBlock struct {
	Kind    string     `json:"kind"`
	Title   string     `json:"title"`
	Text    string     `json:"text,omitempty"`
	Value   string     `json:"value,omitempty"`
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
}

func decodePackageJSON(data []byte, result any) error {
	if !utf8.Valid(data) {
		return errors.New("需要有效的 UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(result); err != nil {
		return errors.New("JSON 格式错误或包含未知字段")
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return errors.New("JSON 只能包含一个值")
	}
	return nil
}

func normalizeManifest(m *PackageManifest, id string) error {
	if m.SchemaVersion != 2 || m.ID != id || !pluginID.MatchString(m.ID) ||
		!validText(m.Name, 64, true) || !validText(m.Version, 24, true) ||
		!validText(m.Description, 220, false) || len(m.Fields) > 16 ||
		len(m.Actions) == 0 || len(m.Actions) > 12 {
		return errors.New("清单版本、ID（须等于 ZIP 文件名）、名称、字段或操作无效")
	}
	if len(m.Capabilities) > 1 || (len(m.Capabilities) == 1 && m.Capabilities[0] != DownloaderSummaryCapability) {
		return errors.New("插件声明了不受支持的能力")
	}
	m.Capabilities = append([]string{}, m.Capabilities...)
	seen := make(map[string]bool)
	for _, action := range m.Actions {
		if !pluginID.MatchString(action.ID) || !validText(action.Label, 64, true) || seen[action.ID] {
			return errors.New("插件操作 ID 或标签无效")
		}
		seen[action.ID] = true
	}
	m.Fields = append([]Field{}, m.Fields...)
	clear(seen)
	for index := range m.Fields {
		field := &m.Fields[index]
		if !pluginID.MatchString(field.Key) || !validText(field.Label, 64, true) || seen[field.Key] ||
			len(field.Options) > 32 || (field.Type != "string" && len(field.Options) > 0) {
			return errors.New("配置字段的键、标签或选项无效")
		}
		seen[field.Key] = true
		options := make(map[string]bool)
		for _, option := range field.Options {
			if !validText(option, 100, true) || options[option] {
				return errors.New("配置字段的选项无效")
			}
			options[option] = true
		}
		if field.Default == nil {
			switch field.Type {
			case "string":
				field.Default = ""
				if len(field.Options) > 0 {
					field.Default = field.Options[0]
				}
			case "boolean":
				field.Default = false
			case "integer":
				field.Default = int64(0)
			}
		}
		value, ok := normalizeSetting(*field, field.Default)
		if !ok {
			return errors.New("配置字段类型或默认值不受支持")
		}
		field.Default = value
	}
	return nil
}

func normalizeSetting(field Field, value any) (any, bool) {
	switch field.Type {
	case "string":
		text, ok := value.(string)
		return text, ok && validText(text, 400, false) && (len(field.Options) == 0 || slices.Contains(field.Options, text))
	case "boolean":
		flag, ok := value.(bool)
		return flag, ok
	case "integer":
		return safeInteger(value)
	}
	return nil, false
}

func safeInteger(value any) (int64, bool) {
	const max = 1<<53 - 1
	var number int64
	switch v := value.(type) {
	case json.Number:
		var err error
		number, err = v.Int64()
		if err != nil {
			return 0, false
		}
	case float64:
		if math.IsNaN(v) || v > max || v < -max || math.Trunc(v) != v {
			return 0, false
		}
		number = int64(v)
	case int:
		number = int64(v)
	case int64:
		number = v
	default:
		return 0, false
	}
	return number, number >= -max && number <= max
}

func effectiveSettings(m PackageManifest, values map[string]any) map[string]any {
	result := make(map[string]any, len(m.Fields))
	for _, field := range m.Fields {
		value, ok := normalizeSetting(field, values[field.Key])
		if !ok {
			value = field.Default
		}
		result[field.Key] = value
	}
	return result
}

func validateResult(result *ActionResult) error {
	if !validText(result.Message, 400, false) || len(result.Blocks) > maxBlocks {
		return ErrPackageExecution
	}
	for _, block := range result.Blocks {
		if !validText(block.Title, 100, true) {
			return ErrPackageExecution
		}
		switch block.Kind {
		case "text":
			if !validText(block.Text, 2000, true) || block.Value != "" || len(block.Columns)+len(block.Rows) != 0 {
				return ErrPackageExecution
			}
		case "metric":
			if !validText(block.Value, 128, true) || !validText(block.Text, 200, false) || len(block.Columns)+len(block.Rows) != 0 {
				return ErrPackageExecution
			}
		case "table":
			if block.Text != "" || block.Value != "" || len(block.Columns) == 0 || len(block.Columns) > 12 || len(block.Rows) > 100 {
				return ErrPackageExecution
			}
			for _, title := range block.Columns {
				if !validText(title, 64, true) {
					return ErrPackageExecution
				}
			}
			for _, row := range block.Rows {
				if len(row) != len(block.Columns) {
					return ErrPackageExecution
				}
				for _, cell := range row {
					if !validText(cell, 400, false) {
						return ErrPackageExecution
					}
				}
			}
		default:
			return ErrPackageExecution
		}
	}
	if result.Message == "" && len(result.Blocks) == 0 {
		return fmt.Errorf("%w：没有返回内容", ErrPackageExecution)
	}
	if result.Blocks == nil {
		result.Blocks = []ResultBlock{}
	}
	return nil
}
