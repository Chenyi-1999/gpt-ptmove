package main

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var errInvalidQBAdd = errors.New("invalid qB torrent upload")

// The original body is forwarded. Require the multipart framing used by
// AddTorrentDataWithOptions to exclude ambiguous headers and extra body data.
func validateQBAdd(contentType string, body []byte) error {
	_, err := qbAddBody(contentType, body, false)
	return err
}

func qbAddBody(contentType string, body []byte, forUpstream bool) ([]byte, error) {
	if len(body) > maxTorrentRequestBytes {
		return nil, errInvalidQBAdd
	}
	typ, params, err := mime.ParseMediaType(contentType)
	if err != nil || typ != "multipart/form-data" {
		return nil, errInvalidQBAdd
	}
	boundary := params["boundary"]
	var canonical bytes.Buffer
	writer := multipart.NewWriter(&canonical)
	if writer.SetBoundary(boundary) != nil || contentType != writer.FormDataContentType() {
		return nil, errInvalidQBAdd
	}

	seen := make(map[string]bool)
	paused := ""
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errInvalidQBAdd
		}
		name := part.FormName()
		if seen[name] {
			return nil, errInvalidQBAdd
		}
		seen[name] = true

		if name == "torrents" {
			filename := part.FileName()
			if len(filename) <= len(".torrent") || len(filename) > 255 ||
				!strings.HasSuffix(strings.ToLower(filename), ".torrent") ||
				strings.ContainsAny(filename, "/\\") || !validQBAddText(filename) {
				return nil, errInvalidQBAdd
			}
			fileWriter, err := writer.CreateFormFile(name, filename)
			if err != nil {
				return nil, errInvalidQBAdd
			}
			start := canonical.Len()
			// The request size limit also bounds this file. qB checks the
			// metainfo itself after the bridge rejects non-dictionary input.
			if _, err := io.Copy(fileWriter, part); err != nil {
				return nil, errInvalidQBAdd
			}
			torrent := canonical.Bytes()[start:]
			if len(torrent) <= 2 || torrent[0] != 'd' || torrent[len(torrent)-1] != 'e' {
				return nil, errInvalidQBAdd
			}
			continue
		}

		const maxFieldBytes = 4 << 10
		value, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
		if err != nil || len(value) > maxFieldBytes || !validQBAddField(name, string(value)) {
			return nil, errInvalidQBAdd
		}
		if name == "paused" {
			paused = string(value)
		}
		if writer.WriteField(name, string(value)) != nil {
			return nil, errInvalidQBAdd
		}
	}
	if !seen["torrents"] || writer.Close() != nil || !bytes.Equal(body, canonical.Bytes()) {
		return nil, errInvalidQBAdd
	}
	if !forUpstream || paused == "" {
		return body, nil
	}

	// qBittorrent 5.3 reads stopped. Preserve paused for older releases, and
	// append its equivalent modern field after validating the controller form.
	var tail bytes.Buffer
	upstream := multipart.NewWriter(&tail)
	if upstream.SetBoundary(boundary) != nil || upstream.WriteField("stopped", paused) != nil || upstream.Close() != nil {
		return nil, errInvalidQBAdd
	}
	closing := []byte("--" + boundary + "--\r\n")
	prefix, ok := bytes.CutSuffix(body, closing)
	if !ok {
		return nil, errInvalidQBAdd
	}
	var forwarded bytes.Buffer
	forwarded.Grow(len(prefix) + tail.Len())
	forwarded.Write(prefix)
	forwarded.Write(tail.Bytes())
	return forwarded.Bytes(), nil
}

func validQBAddField(name, value string) bool {
	if !validQBAddText(value) {
		return false
	}
	switch name {
	case "savepath":
		return validQBSavePath(value)
	case "paused", "skip_checking":
		return value == "true" || value == "false"
	case "tags", "category":
		return true
	case "upLimit":
		limit, err := strconv.ParseInt(value, 10, 64)
		return err == nil && limit > 0 && value == strconv.FormatInt(limit, 10)
	default:
		return false
	}
}

func validQBAddText(value string) bool {
	return utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) == -1
}

func validQBSavePath(value string) bool {
	// The Hub and qB may run on different operating systems.
	// Check POSIX and drive-absolute paths without rewriting either form.
	slashPath := strings.ReplaceAll(value, "\\", "/")
	switch {
	case strings.HasPrefix(value, "/") && !strings.HasPrefix(slashPath, "//"):
	case len(slashPath) >= 3 && slashPath[1:3] == ":/" &&
		strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", rune(slashPath[0])):
		slashPath = slashPath[2:]
		if strings.HasPrefix(slashPath, "//") || strings.ContainsAny(slashPath, `:*?"<>|`) {
			return false
		}
	default:
		return false
	}
	for _, segment := range strings.Split(slashPath, "/") {
		if strings.TrimRight(segment, " ") == ".." {
			return false
		}
	}
	return true
}
