package extract

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	quotedMediaInfoTag   = regexp.MustCompile(`(?is)\[(/?)quote(?:\s*=[^\]]*)?\s*\]`)
	mediaInfoQuoteEmbed  = regexp.MustCompile(`(?i)\[/?(?:img|url)(?:[=\s][^\]]*)?\]`)
	mediaInfoQuoteHeader = regexp.MustCompile(`(?i)^(General|Video|Audio)(?:\s*#\d+)?$`)
	mediaInfoQuoteField  = regexp.MustCompile(`(?i)^\s*(Format|File size|Duration|Codec ID|Width|Height|Channel\(s\)|Sampling rate)\s*:\s*(.*?)\s*$`)
)

type mediaInfoQuoteSection struct {
	name   string
	fields map[string][]string
}

// ExtractQuotedMediaInfo returns a structurally complete MediaInfo report found
// in a BBCode quote. Reports without a General heading receive one so callers
// can pass the result through the usual MediaInfo validation.
func ExtractQuotedMediaInfo(description string) string {
	var selected string
	for _, body := range extractTopLevelMediaInfoQuotes(description) {
		if mediaInfoQuoteEmbed.MatchString(body) {
			continue
		}
		candidate := normalizeMediaInfoQuote(body)
		candidate, ok := completeMediaInfoQuote(candidate)
		if !ok {
			continue
		}
		if selected != "" && candidate != selected {
			return ""
		}
		selected = candidate
	}
	return selected
}

func extractTopLevelMediaInfoQuotes(description string) []string {
	tags := quotedMediaInfoTag.FindAllStringSubmatchIndex(description, -1)
	quotes := make([]string, 0, len(tags)/2)
	depth := 0
	nested := false
	contentStart := 0
	for _, tag := range tags {
		isClosing := description[tag[2]:tag[3]] == "/"
		if !isClosing {
			if depth == 0 {
				contentStart = tag[1]
				nested = false
			} else {
				nested = true
			}
			depth++
			continue
		}
		if depth == 0 {
			continue
		}
		depth--
		if depth == 0 && !nested {
			quotes = append(quotes, description[contentStart:tag[0]])
		}
	}
	return quotes
}

func normalizeMediaInfoQuote(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmed, "\\") {
			trimmed = strings.TrimSuffix(trimmed, "\\")
		}
		lines[i] = trimmed
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func completeMediaInfoQuote(report string) (string, bool) {
	sections, ok := parseMediaInfoQuoteSections(report)
	if !ok {
		return "", false
	}

	hasGeneral := false
	videoCount := 0
	audioCount := 0
	for i, section := range sections {
		switch section.name {
		case "general":
			if hasGeneral || i != 0 {
				return "", false
			}
			hasGeneral = true
		case "video":
			videoCount++
		case "audio":
			audioCount++
		}
	}
	if videoCount == 0 || audioCount == 0 {
		return "", false
	}
	if !hasGeneral {
		if sections[0].name != "" || !hasGeneralMediaInfoFields(sections[0].fields) {
			return "", false
		}
		report = "General\n" + report
	}
	if !hasGeneralMediaInfoFields(sections[0].fields) {
		return "", false
	}
	for _, section := range sections {
		switch section.name {
		case "video":
			if !hasVideoMediaInfoFields(section.fields) {
				return "", false
			}
		case "audio":
			if !hasAudioMediaInfoFields(section.fields) {
				return "", false
			}
		}
	}
	return report, true
}

func parseMediaInfoQuoteSections(report string) ([]mediaInfoQuoteSection, bool) {
	sections := make([]mediaInfoQuoteSection, 0, 4)
	current := mediaInfoQuoteSection{name: "", fields: make(map[string][]string)}
	for _, rawLine := range strings.Split(report, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if header := mediaInfoQuoteHeader.FindStringSubmatch(line); len(header) > 0 {
			if current.name != "" || len(current.fields) > 0 {
				sections = append(sections, current)
			}
			current = mediaInfoQuoteSection{name: strings.ToLower(header[1]), fields: make(map[string][]string)}
			continue
		}
		field := mediaInfoQuoteField.FindStringSubmatch(rawLine)
		if len(field) == 0 || strings.TrimSpace(field[2]) == "" {
			continue
		}
		key := strings.ToLower(field[1])
		current.fields[key] = append(current.fields[key], strings.TrimSpace(field[2]))
	}
	if current.name != "" || len(current.fields) > 0 {
		sections = append(sections, current)
	}
	if len(sections) == 0 {
		return nil, false
	}
	for _, section := range sections {
		if section.name == "" && len(section.fields) == 0 {
			return nil, false
		}
	}
	return sections, true
}

func hasGeneralMediaInfoFields(fields map[string][]string) bool {
	return hasMediaInfoField(fields, "format") &&
		(hasMediaInfoField(fields, "file size") || hasMediaInfoField(fields, "duration"))
}

func hasVideoMediaInfoFields(fields map[string][]string) bool {
	if !hasMediaInfoField(fields, "format") && !hasMediaInfoField(fields, "codec id") {
		return false
	}
	width, widthOK := positiveMediaInfoInteger(fields, "width")
	height, heightOK := positiveMediaInfoInteger(fields, "height")
	return widthOK && heightOK && width >= 16 && height >= 16
}

func hasAudioMediaInfoFields(fields map[string][]string) bool {
	if !hasMediaInfoField(fields, "format") && !hasMediaInfoField(fields, "codec id") {
		return false
	}
	_, hasChannels := positiveMediaInfoInteger(fields, "channel(s)")
	_, hasSampleRate := positiveMediaInfoInteger(fields, "sampling rate")
	return hasChannels || hasSampleRate
}

func hasMediaInfoField(fields map[string][]string, key string) bool {
	for _, value := range fields[key] {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func positiveMediaInfoInteger(fields map[string][]string, key string) (int, bool) {
	for _, raw := range fields[key] {
		digits := strings.Builder{}
		for _, char := range raw {
			switch {
			case char >= '0' && char <= '9':
				digits.WriteRune(char)
			case (char == ' ' || char == ',') && digits.Len() > 0:
				continue
			default:
				goto parseValue
			}
		}
	parseValue:
		if digits.Len() == 0 {
			continue
		}
		value, err := strconv.Atoi(digits.String())
		if err == nil && value > 0 {
			return value, true
		}
	}
	return 0, false
}
