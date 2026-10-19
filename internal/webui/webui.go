// Package webui turns the built web UI (package frontend) into GitView's
// pages: the same React app for every report and for `gitview serve`, with
// the data of the page in an inert JSON block and a Content-Security-Policy
// that pins the app's inline script by its SHA-256.
//
// An offline page (default-src 'none', no connect-src) works from file://
// and never contacts a server; a live page may call back only the local
// server that served it ('self').
package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"
	"time"

	"github.com/athos/gitview/frontend"
)

// Section names of the envelope, read by the app's views.
const (
	SectionGraph    = "graph"    // Repository Graph: Architecture and HTTP
	SectionSecurity = "security" // CodeAudit Security Journey
	SectionReport   = "report"   // session report: Changes, Tests, Code Flow, Inspector, Study
	SectionRuntime  = "runtime"  // Runtime Performance capture
)

// Document is one page.
type Document struct {
	// Kind names what the page is: graph, security, report, runtime or serve.
	Kind       string
	Title      string
	Repository string
	Generated  time.Time
	// View is the view opened when the URL names none (e.g. "security").
	View string
	// Sections hold the data, by section name; each value is marshalled to
	// JSON (json.RawMessage is embedded as is, after validation).
	Sections map[string]any
	// Offered lists the sections a live page can fetch from its server.
	Offered []string
	// Sources is the base64 of the gzip-compressed JSON object path →
	// content of the Go files the code views show; empty for none.
	Sources string
	// Live is set when the page is served by `gitview serve`.
	Live *Live
}

// Live configures a page served locally: the token its API calls carry.
type Live struct {
	Token string `json:"token"`
}

type envelope struct {
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Repository string         `json:"repository"`
	Generated  time.Time      `json:"generated"`
	View       string         `json:"view,omitempty"`
	Sections   map[string]any `json:"sections"`
	Offered    []string       `json:"offered,omitempty"`
	Live       *Live          `json:"live,omitempty"`
}

// page is the built app split where the document is completed.
type page struct {
	head, body, tail string // before <title>…</title>, between it and </body>, from </body>
	hashes           []string
}

var built = mustParse(frontend.Index)

func mustParse(index []byte) page {
	p, err := parse(string(index))
	if err != nil {
		panic("webui: frontend/dist/index.html: " + err.Error() + " (run `make web`)")
	}
	return p
}

const titleTag = "<title>GitView</title>"

func parse(doc string) (page, error) {
	var p page
	t := strings.Index(doc, titleTag)
	b := strings.LastIndex(doc, "</body>")
	if t < 0 || b < 0 || b < t {
		return p, errors.New("not a GitView build: <title>GitView</title> or </body> missing")
	}
	p.head, p.body, p.tail = doc[:t], doc[t+len(titleTag):b], doc[b:]
	// Every script of the build is inline; each one is pinned by its hash.
	rest := doc
	for {
		i := strings.Index(rest, "<script")
		if i < 0 {
			break
		}
		open := strings.IndexByte(rest[i:], '>')
		if open < 0 {
			return p, errors.New("unterminated <script> tag")
		}
		tag := rest[i : i+open+1]
		if strings.Contains(tag, " src=") {
			return p, errors.New("external script in the build: " + tag)
		}
		body := rest[i+open+1:]
		end := strings.Index(body, "</script>")
		if end < 0 {
			return p, errors.New("unterminated script")
		}
		p.hashes = append(p.hashes, hash(body[:end]))
		rest = body[end+len("</script>"):]
	}
	if len(p.hashes) == 0 {
		return p, errors.New("no inline script")
	}
	return p, nil
}

func hash(js string) string {
	sum := sha256.Sum256([]byte(js))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// CSP is the page's Content-Security-Policy; live pages may also call the
// server that served them.
func CSP(live bool) string {
	connect := ""
	if live {
		connect = " connect-src 'self';"
	}
	return fmt.Sprintf("default-src 'none'; script-src %s;%s style-src 'unsafe-inline'; img-src data: blob:; font-src data:; base-uri 'none'; form-action 'none'",
		strings.Join(built.hashes, " "), connect)
}

// Render returns the complete page.
func Render(d Document) ([]byte, error) {
	env := envelope{Kind: d.Kind, Title: d.Title, Repository: d.Repository, Generated: d.Generated,
		View: d.View, Sections: d.Sections, Offered: d.Offered, Live: d.Live}
	if env.Sections == nil {
		env.Sections = map[string]any{}
	}
	if env.Generated.IsZero() {
		env.Generated = time.Now()
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	// <, >, & (and U+2028/9) become \u escapes, so repository content can
	// never close the data block; RawMessage sections are escaped too.
	var data bytes.Buffer
	json.HTMLEscape(&data, raw)

	title := d.Title
	if title == "" {
		title = "GitView"
	}
	var b bytes.Buffer
	b.Grow(len(built.head) + len(built.body) + data.Len() + len(d.Sources) + 512)
	b.WriteString(insertCSP(built.head, CSP(d.Live != nil)))
	b.WriteString("<title>" + html.EscapeString(title) + "</title>")
	b.WriteString(built.body)
	b.WriteString(`<script type="application/json" id="gitview-data">`)
	b.Write(data.Bytes())
	b.WriteString("</script>\n")
	if d.Sources != "" {
		// Base64 only uses [A-Za-z0-9+/=]: inert inside a non-script type.
		b.WriteString(`<script type="application/gzip+base64" id="gitview-sources">`)
		b.WriteString(d.Sources)
		b.WriteString("</script>\n")
	}
	b.WriteString(built.tail)
	return b.Bytes(), nil
}

// Write writes the complete page.
func Write(w io.Writer, d Document) error {
	data, err := Render(d)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// insertCSP puts the policy right after the charset declaration, before any
// script of the head.
func insertCSP(head, csp string) string {
	meta := `<meta http-equiv="Content-Security-Policy" content="` + html.EscapeString(csp) + `">`
	if i := strings.Index(head, "<meta charset"); i >= 0 {
		if j := strings.IndexByte(head[i:], '>'); j >= 0 {
			k := i + j + 1
			return head[:k] + "\n    " + meta + head[k:]
		}
	}
	if i := strings.Index(head, "<head>"); i >= 0 {
		return head[:i+6] + meta + head[i+6:]
	}
	return meta + head
}
