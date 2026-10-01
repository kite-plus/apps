package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
)

// maxIcon is the most an icon may weigh: it is drawn the size of a word.
const maxIcon = 64 << 10

// iconTypes are the pictures an icon may be, by extension.
var iconTypes = map[string]string{
	".svg": "image/svg+xml", ".png": "image/png", ".webp": "image/webp", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
}

// iconPath reports whether p can name an icon in a repository.
func iconPath(p string) bool {
	return p == path.Clean(p) && !path.IsAbs(p) && !strings.HasPrefix(p, "..") &&
		iconTypes[strings.ToLower(path.Ext(p))] != ""
}

// icon points the index at the icon an entry names, as the package's
// repository has it at its newest listed version, from jsDelivr, which
// serves a tag as it was tagged. The file is read again only when that
// address changes, and listed only once a browser can show it without
// running anything.
func (r *runner) icon(ctx context.Context, e *entry, a *app) {
	if e.Icon == "" || len(a.Versions) == 0 {
		a.Icon = ""
		return
	}
	tag := "v" + a.Versions[0].Version
	addr := jsDelivr(e.Repo, tag, e.Icon)
	if a.Icon == addr {
		return
	}
	data, err := r.gh.file(ctx, e.Repo, tag, e.Icon, maxIcon)
	if err == nil {
		err = checkIcon(e.Icon, data)
	}
	if err != nil {
		r.out.failed = append(r.out.failed, fmt.Sprintf("%s: icon %s at %s: %v", e.key(), e.Icon, tag, err))
		if !slices.Contains(r.out.blocking, e.key()) {
			r.out.blocking = append(r.out.blocking, e.key())
		}
		a.Icon = ""
		return
	}
	a.Icon = addr
}

// checkIcon reports whether data is a picture a browser shows without
// running anything: the PNG, WebP or JPEG its name says, or an SVG with no
// scripts, no event handlers, nothing embedded and nothing fetched.
func checkIcon(name string, data []byte) error {
	want := iconTypes[strings.ToLower(path.Ext(name))]
	if want != "image/svg+xml" {
		if got := http.DetectContentType(data); got != want {
			return fmt.Errorf("it is %s, not %s", got, want)
		}
		return nil
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	root, inStyle := "", false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("it is not SVG: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if root == "" {
				if root = t.Name.Local; root != "svg" {
					return fmt.Errorf("it is <%s>, not SVG", root)
				}
			}
			switch strings.ToLower(t.Name.Local) {
			case "script", "foreignobject", "iframe", "object", "embed", "audio", "video":
				return fmt.Errorf("it has a <%s>", t.Name.Local)
			}
			inStyle = t.Name.Local == "style"
			for _, at := range t.Attr {
				if err := checkSVGValue(t.Name.Local, at); err != nil {
					return err
				}
			}
		case xml.EndElement:
			inStyle = false
		case xml.CharData:
			if inStyle && fetches(string(t)) {
				return errors.New("its <style> fetches from elsewhere")
			}
		}
	}
	if root == "" {
		return errors.New("it is not SVG")
	}
	return nil
}

// checkSVGValue refuses an attribute that runs something or fetches from
// elsewhere: an event handler, a link that is not to the picture itself or
// a picture inside it, or a style that loads a URL.
func checkSVGValue(el string, at xml.Attr) error {
	name := strings.ToLower(at.Name.Local)
	value := strings.ToLower(strings.TrimSpace(at.Value))
	switch {
	case strings.HasPrefix(name, "on"):
		return fmt.Errorf("<%s> has an event handler, %s", el, at.Name.Local)
	case name == "href" && !strings.HasPrefix(value, "#") && !strings.HasPrefix(value, "data:image/"):
		return fmt.Errorf("<%s> links to %s", el, at.Value)
	case fetches(value):
		return fmt.Errorf("<%s> %s fetches from elsewhere", el, at.Name.Local)
	}
	return nil
}

// fetches reports whether CSS loads something from outside the picture:
// an @import, or a url() that is not a fragment of it.
func fetches(css string) bool {
	css = strings.ToLower(css)
	if strings.Contains(css, "@import") {
		return true
	}
	for rest := css; ; {
		i := strings.Index(rest, "url(")
		if i < 0 {
			return false
		}
		rest = strings.TrimLeft(rest[i+len("url("):], " \t\n'\"")
		if !strings.HasPrefix(rest, "#") {
			return true
		}
	}
}
