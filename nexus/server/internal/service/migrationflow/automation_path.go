package migrationflow

import "strings"

// sameAutomationSavePath 忽略下载器补齐的尾部分隔符，但保留根目录和盘符根目录的语义。
func sameAutomationSavePath(left, right string) bool {
	normalize := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		trimmed := strings.TrimRight(value, "/\\")
		if trimmed == "" {
			return "/"
		}
		if len(trimmed) == 2 && trimmed[1] == ':' && len(value) > len(trimmed) {
			return trimmed + "/"
		}
		return trimmed
	}
	return normalize(left) == normalize(right)
}
