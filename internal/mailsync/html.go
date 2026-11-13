// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"bytes"
	"golang.org/x/net/html"
	"strings"
)

// HTMLText extracts bounded inert text; it never renders markup or fetches a
// resource. Native text/plain is preferred by Parse when both alternatives exist.
func HTMLText(raw []byte) (string, error) {
	if len(raw) > maxText {
		return "", ErrMIME
	}
	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return "", ErrMIME
	}
	var out strings.Builder
	nodes := 0
	var visit func(*html.Node, int) error
	visit = func(node *html.Node, depth int) error {
		nodes++
		if nodes > 10000 || depth > 96 {
			return ErrMIME
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "head", "iframe", "object", "embed", "svg", "math", "template", "form", "noscript":
				return nil
			}
			for _, attribute := range node.Attr {
				if attribute.Key == "hidden" {
					return nil
				}
			}
		}
		block := false
		if node.Type == html.ElementNode {
			switch node.Data {
			case "p", "div", "section", "article", "header", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "ul", "ol", "li", "tr", "pre", "br", "hr":
				block = true
			}
		}
		if block {
			out.WriteByte('\n')
		}
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		if block {
			out.WriteByte('\n')
		} else if node.Type == html.ElementNode && (node.Data == "td" || node.Data == "th") {
			out.WriteByte(' ')
		}
		if out.Len() > 2*maxText {
			return ErrMIME
		}
		return nil
	}
	if err = visit(root, 0); err != nil {
		return "", err
	}
	lines := []string{}
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	return text(strings.Join(lines, "\n"), maxText), nil
}
