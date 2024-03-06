package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"github.com/google/subcommands"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type staticCmd struct {
}

func (*staticCmd) Name() string     { return "static" }
func (*staticCmd) Synopsis() string { return "generate static HTML files for all pages" }
func (*staticCmd) Usage() string {
	return `static <dir name>:
  Create static copies in the given directory.
`
}

func (cmd *staticCmd) SetFlags(f *flag.FlagSet) {
}

func (cmd *staticCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	args := f.Args()
	if len(args) != 1 {
		fmt.Println("Exactly one target directory is required")
		return subcommands.ExitFailure
	}
	return staticCli(filepath.Clean(args[0]), false)
}

// staticCli generates a static site in the designated directory. The quiet flag is used to suppress output when running
// tests.
func staticCli(dir string, quiet bool) subcommands.ExitStatus {
	index.load()
	index.RLock()
	defer index.RUnlock()
	loadLanguages()
	loadTemplates()
	n := 0
	err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		n++
		if !quiet && (n < 100 || n < 1000 && n%10 == 0 || n%100 == 0) {
			fmt.Fprintf(os.Stdout, "\r%d", n)
		}
		return staticFile(path, dir, info, err)
	})
	if !quiet {
		fmt.Printf("\r%d\n", n)
	}
	if err != nil {
		fmt.Println(err)
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

// staticFile is used to walk the file trees and do the right thing for the destination directory: create
// subdirectories, link files, render HTML files.
func staticFile(path, dir string, info fs.FileInfo, err error) error {
	if err != nil {
		return err
	}
	// skip hidden directories and files
	if path != "." && strings.HasPrefix(filepath.Base(path), ".") {
		if info.IsDir() {
			return filepath.SkipDir
		} else {
			return nil
		}
	}
	// skip backup files, avoid recursion
	if strings.HasSuffix(path, "~") || strings.HasPrefix(path, dir) {
		return nil
	}
	// recreate subdirectories
	if info.IsDir() {
		return os.Mkdir(filepath.Join(dir, path), 0755)
	}
	// render pages
	if strings.HasSuffix(path, ".md") {
		p, err := staticPage(path, dir)
		if err != nil {
			return err
		}
		return staticFeed(path, dir, p, info.ModTime())
	}
	// remaining files are linked
	return os.Link(path, filepath.Join(dir, path))
}

// staticPage takes the filename of a page (ending in ".md") and generates a static HTML page.
func staticPage(path, dir string) (*Page, error) {
	name := strings.TrimSuffix(path, ".md")
	p, err := loadPage(filepath.ToSlash(name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot load %s: %s\n", name, err)
		return nil, err
	}
	p.handleTitle(true)
	// instead of p.renderHtml() we do it all ourselves, appending ".html" to all the local links
	parser, hashtags := wikiParser()
	doc := markdown.Parse(p.Body, parser)
	ast.WalkFunc(doc, staticLinks)
	opts := html.RendererOptions{
		Flags: html.CommonFlags,
	}
	renderer := html.NewRenderer(opts)
	maybeUnsafeHTML := markdown.Render(doc, renderer)
	p.Name = nameEscape(p.Name)
	p.Html = unsafeBytes(maybeUnsafeHTML)
	p.Language = language(p.plainText())
	p.Hashtags = *hashtags
	return p, p.write(filepath.Join(dir, name+".html"))
}

// staticFeed writes a .rss file for a page, but only if it's an index page or a page that might be used as a hashtag
func staticFeed(path, dir string, p *Page, ti time.Time) error {
	// render feed, maybe
	name := strings.TrimSuffix(path, ".md")
	base := filepath.Base(name)
	_, ok := index.token["#"+strings.ToLower(base)]
	if base == "index" || ok {
		f := feed(p, ti)
		fp := filepath.Join(dir, name + ".rss")
		dst, err := os.Create(fp)
		if err != nil {
			return err
		}
		_, err = dst.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>`))
		if err != nil {
			return err
		}
		templates.RLock()
		defer templates.RUnlock()
		t := templates.template["feed.html"]
		return t.Execute(dst, f)
	}
	return nil
}

// staticLinks checks a node and if it is a link to a local page, it appends ".html" to the link destination.
func staticLinks(node ast.Node, entering bool) ast.WalkStatus {
	if entering {
		switch v := node.(type) {
		case *ast.Link:
			// not an absolute URL, not a full URL, not a mailto: URI
			if !bytes.HasPrefix(v.Destination, []byte("/")) &&
				!bytes.Contains(v.Destination, []byte("://")) &&
				!bytes.HasPrefix(v.Destination, []byte("mailto:")) {
				// pointing to a page file (instead of an image file, for example).
				fn, err := url.PathUnescape(string(v.Destination))
				if err != nil {
					return ast.GoToNext
				}
				_, err = os.Stat(fn + ".md")
				if err != nil {
					return ast.GoToNext
				}
				v.Destination = append(v.Destination, []byte(".html")...)
			}
		}
	}
	return ast.GoToNext
}

func (p *Page) write(destination string) error {
	t := "static.html"
	f, err := os.Create(destination)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create %s.html: %s\n", destination, err)
		return err
	}
	err = templates.template[t].Execute(f, p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot execute %s template for %s: %s\n", t, destination, err)
		return err
	}
	return nil
}
