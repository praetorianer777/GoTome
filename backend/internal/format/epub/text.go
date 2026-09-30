package epub

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/praetorianer777/gotome/backend/internal/format/markup"
)

// replacement stands in for bytes that are not UTF-8.
var replacement = []byte(string(utf8.RuneError))

type navLink struct{ href, text string }

// navLinks returns the links of the table of contents in an EPUB 3 navigation
// document: the nav element whose epub:type is toc, or every link when the
// document marks none.
func navLinks(doc []byte) []navLink {
	root, err := html.Parse(bytes.NewReader(bytes.ToValidUTF8(doc, replacement)))
	if err != nil {
		return nil
	}
	toc := findNode(root, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.DataAtom != atom.Nav {
			return false
		}
		for _, a := range n.Attr {
			if a.Key == "epub:type" || a.Key == "type" {
				for _, v := range strings.Fields(a.Val) {
					if v == "toc" {
						return true
					}
				}
			}
		}
		return false
	})
	if toc == nil {
		toc = root
	}

	var links []navLink
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			for _, a := range n.Attr {
				if a.Key == "href" {
					var tb markup.Builder
					collectText(n, &tb)
					links = append(links, navLink{href: a.Val, text: tb.String()})
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(toc)
	return links
}

func findNode(n *html.Node, match func(*html.Node) bool) *html.Node {
	if match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findNode(c, match); found != nil {
			return found
		}
	}
	return nil
}

func collectText(n *html.Node, tb *markup.Builder) {
	if n.Type == html.TextNode {
		tb.Write(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, tb)
	}
}
