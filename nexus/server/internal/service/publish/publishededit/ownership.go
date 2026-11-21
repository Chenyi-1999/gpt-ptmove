package publishededit

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func resolveNovaLink(base, raw string) *url.URL {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	origin, err := url.Parse(base)
	if err != nil {
		return nil
	}
	resolved, err := origin.Parse(strings.TrimSpace(raw))
	if err != nil || !isNovaURL(resolved) {
		return nil
	}
	return resolved
}

func novaLinkID(href, expectedPath string) string {
	link := resolveNovaLink(novaOrigin, href)
	if link == nil || link.EscapedPath() != expectedPath {
		return ""
	}
	query, err := url.ParseQuery(link.RawQuery)
	if err != nil || len(query["id"]) != 1 || !trackerID.MatchString(query.Get("id")) {
		return ""
	}
	return query.Get("id")
}

func verifyOwnership(page *html.Node, id string) (string, error) {
	var ownerCell *html.Node
	hasEdit := false
	for node := range page.Descendants() {
		if node.Type != html.ElementNode {
			continue
		}
		if node.Data == "a" && hasClass(node, "index") && novaLinkID(attr(node, "href"), "/download.php") == id {
			cell := containingDownloadCell(node)
			if cell == nil {
				continue
			}
			if ownerCell != nil && ownerCell != cell {
				return "", ErrPermission
			}
			ownerCell = cell
		}
		if node.Data == "a" && novaLinkID(attr(node, "href"), "/edit.php") == id && hasTrackerEditIcon(node) {
			hasEdit = true
		}
	}
	if ownerCell == nil || !hasEdit {
		return "", ErrPermission
	}
	currentID := currentUserID(page)
	ownerID := uniqueUserID(ownerCell, "Uploader_Name")
	if ownerID == "" || ownerID != currentID {
		return "", ErrPermission
	}
	return ownerID, nil
}

func currentUserID(page *html.Node) string {
	var userbar *html.Node
	for node := range page.Descendants() {
		if node.Type != html.ElementNode || !hasClass(node, "nova-userbar-hello") {
			continue
		}
		if userbar != nil {
			return ""
		}
		userbar = node
	}
	if userbar == nil {
		return ""
	}
	return uniqueUserID(userbar, "")
}

func hasClass(node *html.Node, class string) bool {
	for _, name := range strings.Fields(attr(node, "class")) {
		if name == class {
			return true
		}
	}
	return false
}

func containingDownloadCell(link *html.Node) *html.Node {
	for parent := link.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "td" {
			if hasClass(parent, "rowfollow") {
				return parent
			}
			return nil
		}
	}
	return nil
}

func hasTrackerEditIcon(anchor *html.Node) bool {
	for node := range anchor.Descendants() {
		if node.Type == html.ElementNode && node.Data == "img" && hasClass(node, "dt_edit") {
			return true
		}
	}
	return false
}

func uniqueUserID(container *html.Node, linkClass string) string {
	result := ""
	if container.Data == "a" && (linkClass == "" || hasClass(container, linkClass)) {
		result = novaLinkID(attr(container, "href"), "/userdetails.php")
	}
	for node := range container.Descendants() {
		if node.Type != html.ElementNode || node.Data != "a" || (linkClass != "" && !hasClass(node, linkClass)) {
			continue
		}
		id := novaLinkID(attr(node, "href"), "/userdetails.php")
		if id != "" {
			if result != "" && result != id {
				return ""
			}
			result = id
		}
	}
	return result
}
