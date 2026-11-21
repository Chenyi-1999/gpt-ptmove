package publishededit

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type formField struct {
	Name  string
	Value string
	File  bool
}

type editForm struct {
	action        string
	enctype       string
	fields        []formField
	descrEditable bool
	mediaEditable bool
}

func (form editForm) value(name string) (string, bool) {
	var value string
	count := 0
	for _, field := range form.fields {
		if field.Name == name && !field.File {
			value = field.Value
			count++
		}
	}
	return value, count == 1
}

func readEditForm(content, editURL, id, owner string) (editForm, error) {
	var result editForm
	page, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return result, ErrUnsupported
	}
	if owner == "" || currentUserID(page) != owner {
		return result, ErrPermission
	}
	var form *html.Node
	for node := range page.Descendants() {
		if node.Type != html.ElementNode || node.Data != "form" {
			continue
		}
		action := resolveNovaLink(editURL, attr(node, "action"))
		if action == nil || action.EscapedPath() != "/takeedit.php" || action.RawQuery != "" || action.ForceQuery {
			continue
		}
		if form != nil || attr(node, "id") != "compose" || attr(node, "name") != "edittorrent" ||
			!strings.EqualFold(strings.TrimSpace(attr(node, "method")), "post") {
			return editForm{}, ErrUnsupported
		}
		form = node
	}
	if form == nil {
		return result, ErrUnsupported
	}
	charset := strings.TrimSpace(attr(form, "accept-charset"))
	if charset != "" && !strings.EqualFold(charset, "utf-8") {
		return result, ErrUnsupported
	}
	result.action = novaOrigin + "/takeedit.php"
	result.enctype = strings.ToLower(strings.TrimSpace(attr(form, "enctype")))
	if result.enctype == "" {
		result.enctype = "application/x-www-form-urlencoded"
	}
	if result.enctype != "application/x-www-form-urlencoded" && result.enctype != "multipart/form-data" {
		return editForm{}, ErrUnsupported
	}
	if err := result.readControls(page, form); err != nil {
		return editForm{}, err
	}
	formID, idFound := result.value("id")
	_, descrFound := result.value(descriptionField)
	_, mediaFound := result.value(mediaInfoField)
	nfoAction, nfoFound := result.value("nfoaction")
	if !idFound || formID != id || !descrFound || !result.descrEditable || (result.mediaEditable && !mediaFound) ||
		!nfoFound || nfoAction != "keep" {
		return editForm{}, ErrUnsupported
	}
	return result, nil
}

func (form *editForm) readControls(page, formNode *html.Node) error {
	var submit *html.Node
	for node := range page.Descendants() {
		if node.Type != html.ElementNode || !isFormControl(node) || !belongsToForm(node, formNode) || disabledControl(node) {
			continue
		}
		name := attr(node, "name")
		if isSubmitControl(node) {
			if submit != nil || (name != "" && name != "edit") || hasAttr(node, "formaction") || hasAttr(node, "formmethod") || hasAttr(node, "formenctype") {
				return ErrUnsupported
			}
			submit = node
			if name != "" {
				form.fields = append(form.fields, formField{Name: name, Value: attr(node, "value")})
			}
			continue
		}
		if name == "" {
			continue
		}
		if hasAttr(node, "dirname") || strings.ContainsAny(name, "\r\n\x00") {
			return ErrUnsupported
		}
		if name == "_charset_" && node.Data == "input" {
			form.fields = append(form.fields, formField{Name: name, Value: "UTF-8"})
			continue
		}
		switch node.Data {
		case "input":
			if err := form.readInput(node, name); err != nil {
				return err
			}
		case "textarea":
			if strings.EqualFold(attr(node, "wrap"), "hard") {
				return ErrUnsupported
			}
			writable := !hasAttr(node, "readonly")
			if name == descriptionField {
				form.descrEditable = writable
			}
			if name == mediaInfoField {
				form.mediaEditable = writable
			}
			form.fields = append(form.fields, formField{Name: name, Value: plainText(node)})
		case "select":
			for _, option := range selectedOptions(node) {
				value := attr(option, "value")
				if !hasAttr(option, "value") {
					value = strings.Join(strings.Fields(plainText(option)), " ")
				}
				form.fields = append(form.fields, formField{Name: name, Value: value})
			}
		case "object":
			return ErrUnsupported
		}
	}
	if submit == nil {
		return ErrUnsupported
	}
	return nil
}

func isFormControl(node *html.Node) bool {
	switch node.Data {
	case "input", "textarea", "select", "button", "object":
		return true
	}
	return false
}

func belongsToForm(node, form *html.Node) bool {
	if hasAttr(node, "form") {
		id := attr(form, "id")
		return id != "" && attr(node, "form") == id
	}
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "form" {
			return parent == form
		}
	}
	return false
}

func disabledControl(node *html.Node) bool {
	if hasAttr(node, "disabled") {
		return true
	}
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if parent.Type != html.ElementNode {
			continue
		}
		switch parent.Data {
		case "template", "datalist":
			return true
		case "fieldset", "optgroup":
			if hasAttr(parent, "disabled") {
				return true
			}
		}
	}
	return false
}

func isSubmitControl(node *html.Node) bool {
	kind := strings.ToLower(attr(node, "type"))
	return node.Data == "input" && kind == "submit" || node.Data == "button" && kind != "button" && kind != "reset"
}

func (form *editForm) readInput(node *html.Node, name string) error {
	value := attr(node, "value")
	switch strings.ToLower(attr(node, "type")) {
	case "button", "reset":
		return nil
	case "image", "password":
		return ErrUnsupported
	case "checkbox", "radio":
		if !hasAttr(node, "checked") {
			return nil
		}
		if !hasAttr(node, "value") {
			value = "on"
		}
	case "file":
		if form.enctype != "multipart/form-data" || name != "nfo" || value != "" {
			return ErrUnsupported
		}
		// 和浏览器一样提交空 NFO 文件部分，保留编辑表单当前选择的 NFO 动作。
		form.fields = append(form.fields, formField{Name: name, File: true})
		return nil
	}
	form.fields = append(form.fields, formField{Name: name, Value: value})
	return nil
}

func selectedOptions(selectNode *html.Node) []*html.Node {
	var first *html.Node
	var selected []*html.Node
	for node := range selectNode.Descendants() {
		if node.Type != html.ElementNode || node.Data != "option" {
			continue
		}
		if first == nil && !disabledControl(node) {
			first = node
		}
		if hasAttr(node, "selected") {
			selected = append(selected, node)
		}
	}
	if !hasAttr(selectNode, "multiple") {
		if count := len(selected); count > 1 {
			selected = selected[count-1:]
		}
		size := strings.TrimSpace(attr(selectNode, "size"))
		if len(selected) == 0 && first != nil && (size == "" || size == "0" || size == "1") {
			selected = append(selected, first)
		}
	}
	enabled := selected[:0]
	for _, option := range selected {
		if !disabledControl(option) {
			enabled = append(enabled, option)
		}
	}
	return enabled
}

func encodeEditForm(fields []formField, enctype string) (io.Reader, string, error) {
	if enctype == "application/x-www-form-urlencoded" {
		values := make([]string, 0, len(fields))
		for _, field := range fields {
			values = append(values, url.QueryEscape(field.Name)+"="+url.QueryEscape(browserNewlines(field.Value)))
		}
		return strings.NewReader(strings.Join(values, "&")), enctype, nil
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range fields {
		var err error
		if field.File {
			_, err = writer.CreateFormFile(field.Name, "")
		} else {
			err = writer.WriteField(field.Name, browserNewlines(field.Value))
		}
		if err != nil {
			return nil, "", fmt.Errorf("无法构建 NovaHD 编辑表单: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("无法构建 NovaHD 编辑表单: %w", err)
	}
	return &body, writer.FormDataContentType(), nil
}

func attr(node *html.Node, name string) string {
	for _, item := range node.Attr {
		if item.Namespace == "" && item.Key == name {
			return item.Val
		}
	}
	return ""
}

func hasAttr(node *html.Node, name string) bool {
	for _, item := range node.Attr {
		if item.Namespace == "" && item.Key == name {
			return true
		}
	}
	return false
}

func plainText(node *html.Node) string {
	var result strings.Builder
	for child := range node.Descendants() {
		if child.Type == html.TextNode {
			result.WriteString(child.Data)
		}
	}
	return result.String()
}

func normalizeNewlines(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func browserNewlines(text string) string {
	return strings.ReplaceAll(normalizeNewlines(text), "\n", "\r\n")
}
