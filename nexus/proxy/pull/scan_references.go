package main

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const localScanReferencesURI = "/api/local-scan/references"

type localScanReference struct {
	Hash        string `json:"hash"`
	SavePath    string `json:"save_path"`
	ContentPath string `json:"content_path"`
}

// projectLocalScanReferences 过滤下载器清单，领单回传只保留核对引用所需的字段。
func projectLocalScanReferences(data []byte) ([]byte, error) {
	var refs []localScanReference
	if !utf8.Valid(data) || json.Unmarshal(data, &refs) != nil || refs == nil {
		return nil, errors.New("invalid qB reference list")
	}
	for _, ref := range refs {
		if ref.Hash == "" || ref.SavePath == "" || ref.ContentPath == "" {
			return nil, errors.New("incomplete qB reference")
		}
	}
	return json.Marshal(refs)
}
