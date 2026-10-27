package automation

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Select 在不执行脚本的 HTML 树中匹配声明式选择器；不支持的语法保持空结果。
// 支持标签、类、标识、属性、后代、直接子节点、逗号与简单 :not；空选择器不匹配。
func Select(root *html.Node, selector string) []*html.Node {
	out := []*html.Node{}
	if strings.TrimSpace(selector) == "" {
		return out
	}
	groups := strings.Split(selector, ",")
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, group := range groups {
				parts := selectorParts(group)
				if len(parts) > 0 && matchesChain(n, parts) {
					out = append(out, n)
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}
func selectorParts(s string) []string {
	parts := []string{}
	var b strings.Builder
	depth := 0
	var quote rune
	flush := func() {
		if b.Len() > 0 {
			parts = append(parts, b.String())
			b.Reset()
		}
	}
	for _, c := range strings.TrimSpace(s) {
		if quote != 0 {
			b.WriteRune(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			b.WriteRune(c)
			continue
		}
		if c == '[' || c == '(' {
			depth++
		}
		if c == ']' || c == ')' {
			depth--
		}
		if depth == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>') {
			flush()
			if c == '>' {
				parts = append(parts, ">")
			}
			continue
		}
		b.WriteRune(c)
	}
	flush()
	if depth != 0 || quote != 0 {
		return nil
	}
	return parts
}
func matchesChain(n *html.Node, parts []string) bool {
	if len(parts) == 0 || parts[len(parts)-1] == ">" || !matchesSimple(n, parts[len(parts)-1]) {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	prefix := parts[:len(parts)-1]
	if prefix[len(prefix)-1] == ">" {
		if len(prefix) < 2 || n.Parent == nil {
			return false
		}
		return matchesChain(n.Parent, prefix[:len(prefix)-1])
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if matchesChain(p, prefix) {
			return true
		}
	}
	return false
}

var selectorName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*`)
var attributeSelector = regexp.MustCompile(`^\[([a-zA-Z_][a-zA-Z0-9_-]*)(?:(\*=|\^=|\$=|~=|=)["']?([^"'\]]*)["']?)?\]`)

func matchesSimple(n *html.Node, s string) bool {
	if n.Type != html.ElementNode || s == "" {
		return false
	}
	if s[0] == '*' {
		s = s[1:]
	} else if m := selectorName.FindString(s); m != "" {
		if !strings.EqualFold(n.Data, m) {
			return false
		}
		s = s[len(m):]
	}
	for s != "" {
		switch s[0] {
		case '.', '#':
			kind := s[0]
			m := selectorName.FindString(s[1:])
			if m == "" {
				return false
			}
			if kind == '#' {
				if attr(n, "id") != m {
					return false
				}
			} else {
				found := false
				for _, c := range strings.Fields(attr(n, "class")) {
					if c == m {
						found = true
					}
				}
				if !found {
					return false
				}
			}
			s = s[len(m)+1:]
		case '[':
			m := attributeSelector.FindStringSubmatch(s)
			if len(m) == 0 {
				return false
			}
			value := attr(n, m[1])
			exists := false
			for _, a := range n.Attr {
				if a.Key == m[1] {
					exists = true
				}
			}
			if !exists {
				return false
			}
			switch m[2] {
			case "=":
				if value != m[3] {
					return false
				}
			case "*=":
				if !strings.Contains(value, m[3]) {
					return false
				}
			case "^=":
				if !strings.HasPrefix(value, m[3]) {
					return false
				}
			case "$=":
				if !strings.HasSuffix(value, m[3]) {
					return false
				}
			case "~=":
				if !strings.Contains(" "+value+" ", " "+m[3]+" ") {
					return false
				}
			}
			s = s[len(m[0]):]
		case ':':
			if !strings.HasPrefix(s, ":not(") {
				return false
			}
			end := strings.Index(s, ")")
			if end < 0 {
				return false
			}
			if matchesSimple(n, s[5:end]) {
				return false
			}
			s = s[end+1:]
		default:
			return false
		}
	}
	return true
}
