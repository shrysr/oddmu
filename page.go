package main

import (
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
	"github.com/microcosm-cc/bluemonday"
	"html/template"
	"strings"
	"bytes"
	"os"
)

// Page is a struct containing information about a single page. Title
// is the title extracted from the page content using titleRegexp.
// Name is the filename without extension (so a filename of "foo.md"
// results in the Name "foo"). Body is the Markdown content of the
// page and Html is the rendered HTML for that Markdown. Score is a
// number indicating how well the page matched for a search query.
type Page struct {
	Title string
	Name  string
	Body  []byte
	Html  template.HTML
	Score int
}

// save saves a Page. The filename is based on the Page.Name and gets
// the ".md" extension. Page.Body is saved, without any carriage
// return characters ("\r"). The file permissions used are readable
// and writeable for the current user, i.e. u+rw or 0600. Page.Title
// and Page.Html are not saved no caching. There is no caching.
func (p *Page) save() error {
	filename := p.Name + ".md"
	updateIndex(p)
	return os.WriteFile(filename, bytes.ReplaceAll(p.Body, []byte{'\r'}, []byte{}), 0600)
}

// loadPage loads a Page given a name. The filename loaded is that
// Page.Name with the ".md" extension. The Page.Title is set to the
// Page.Name (and possibly changed, later). The Page.Body is set to
// the file content. The Page.Html remains undefined (there is no
// caching).
func loadPage(name string) (*Page, error) {
	filename := name + ".md"
	body, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return &Page{Title: name, Name: name, Body: body}, nil
}

// handleTitle extracts the title from a Page and sets Page.Title, if
// any. If replace is true, the page title is also removed from
// Page.Body. Make sure not to save this! This is only for rendering.
func (p* Page) handleTitle(replace bool) {
	s := string(p.Body)
	m := titleRegexp.FindStringSubmatch(s)
	if m != nil {
		p.Title = m[1]
		if replace {
			p.Body = []byte(strings.Replace(s, m[0], "", 1))
		}
	}
}

// renderHtml renders the Page.Body to HTML and sets Page.Html.
func (p* Page) renderHtml() {
	maybeUnsafeHTML := markdown.ToHTML(p.Body, nil, nil)
	html := bluemonday.UGCPolicy().SanitizeBytes(maybeUnsafeHTML)
	p.Html = template.HTML(html);
}

// plainText renders the Page.Body to plain text and returns it,
// ignoring all the Markdown and all the newlines. The result is one
// long single line of text.
func (p* Page) plainText() string {
	parser := parser.New()
	doc := markdown.Parse(p.Body, parser)
	text := []byte("")
	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if entering && node.AsLeaf() != nil {
			text = append(text, node.AsLeaf().Literal...)
			text = append(text, []byte(" ")...)
		}
		return ast.GoToNext
	})
	return strings.ReplaceAll(string(text), "\n", " ")
}

// summarize for query string q sets Page.Html to an extract.
func (p* Page) summarize(q string) {
	p.handleTitle(true)
	s, c := snippets(q, p.plainText())
	p.Score = c
	extract := []byte(s)
	html := bluemonday.UGCPolicy().SanitizeBytes(extract)
	p.Html = template.HTML(html)
}