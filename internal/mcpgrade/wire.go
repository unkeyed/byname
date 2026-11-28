package mcpgrade

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	bodyCap     = 2 << 20
	gradeBytes  = 8 << 20
	maxRequests = 40
	maxTools    = 500
)

var (
	errRequests = errors.New("the grade's budget of 40 requests is spent")
	errBodyCap  = errors.New("response body over 2 MiB")
	errGradeCap = errors.New("the grade's budget of 8 MiB read is spent")
)

type wire struct {
	next     http.RoundTripper
	endpoint string
	token    string

	mu        sync.Mutex
	requests  int
	read      int64
	toolPages []*page
}

func newWire(t Target) *wire {
	next := http.DefaultTransport
	if t.Client != nil && t.Client.Transport != nil {
		next = t.Client.Transport
	}
	return &wire{next: next, endpoint: t.URL, token: t.Token}
}

func (w *wire) client(base *http.Client, withToken bool) *http.Client {
	var c http.Client
	if base != nil {
		c = *base
	}
	c.Transport = tap{w: w, withToken: withToken}
	return &c
}

type tap struct {
	w         *wire
	withToken bool
}

func (t tap) RoundTrip(r *http.Request) (*http.Response, error) {
	w := t.w
	w.mu.Lock()
	if w.requests >= maxRequests {
		w.mu.Unlock()
		return nil, errRequests
	}
	w.requests++
	w.mu.Unlock()

	r = r.Clone(r.Context())
	r.Header.Del("Authorization")
	if t.withToken && w.token != "" && exactURL(r.URL, w.endpoint) {
		r.Header.Set("Authorization", "Bearer "+w.token)
	}
	var pg *page
	if isToolsList(r) {
		pg = &page{}
		w.mu.Lock()
		w.toolPages = append(w.toolPages, pg)
		w.mu.Unlock()
	}
	resp, err := w.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	resp.Body = &capped{body: resp.Body, w: w, left: bodyCap, page: pg}
	return resp, nil
}

// exactURL is stricter than sameURL: the token goes only to the endpoint as
// given, not to a redirect that adds a slash or encodes the path differently.
func exactURL(u *url.URL, endpoint string) bool {
	e, err := url.Parse(endpoint)
	return err == nil && strings.EqualFold(u.Scheme, e.Scheme) && strings.EqualFold(u.Host, e.Host) &&
		u.EscapedPath() == e.EscapedPath() && u.RawQuery == e.RawQuery
}

func isToolsList(r *http.Request) bool {
	if r.Method != http.MethodPost || r.GetBody == nil {
		return false
	}
	b, err := r.GetBody()
	if err != nil {
		return false
	}
	defer b.Close()
	var m struct {
		Method string `json:"method"`
	}
	return json.NewDecoder(io.LimitReader(b, 1<<20)).Decode(&m) == nil && m.Method == "tools/list"
}

type capped struct {
	body io.ReadCloser
	w    *wire
	left int64
	page *page
}

type page struct {
	body bytes.Buffer
	err  error
}

func (c *capped) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, c.cut(errBodyCap)
	}
	p = p[:min(len(p), int(c.left))]
	n, err := c.body.Read(p)
	c.left -= int64(n)
	c.w.mu.Lock()
	c.w.read += int64(n)
	over := c.w.read > gradeBytes
	if c.page != nil {
		c.page.body.Write(p[:n])
	}
	c.w.mu.Unlock()
	if over {
		return n, c.cut(errGradeCap)
	}
	return n, err
}

func (c *capped) Close() error { return c.body.Close() }

func (c *capped) cut(err error) error {
	if c.page != nil {
		c.w.mu.Lock()
		c.page.err = err
		c.w.mu.Unlock()
	}
	return err
}

func (w *wire) pages() ([][]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([][]byte, len(w.toolPages))
	var err error
	for i, p := range w.toolPages {
		out[i] = bytes.Clone(p.body.Bytes())
		err = cmp.Or(err, p.err)
	}
	return out, err
}

func messages(body []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		if t := bytes.TrimSpace(body); len(t) > 0 && (t[0] == '{' || t[0] == '[') {
			yield(t)
			return
		}
		var data [][]byte
		sc := bufio.NewScanner(bytes.NewReader(body))
		sc.Buffer(nil, bodyCap)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				if len(data) > 0 && !yield(bytes.Join(data, []byte("\n"))) {
					return
				}
				data = nil
				continue
			}
			if v, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				data = append(data, bytes.Clone(bytes.TrimPrefix(v, []byte(" "))))
			}
		}
		if len(data) > 0 {
			yield(bytes.Join(data, []byte("\n")))
		}
	}
}

func errorCode(body []byte) int64 {
	for m := range messages(body) {
		var msg struct {
			Error *struct {
				Code int64 `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(m, &msg) == nil && msg.Error != nil && msg.Error.Code != 0 {
			return msg.Error.Code
		}
	}
	return 0
}

type wireTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Annotations  struct {
		Title           string `json:"title"`
		ReadOnlyHint    *bool  `json:"readOnlyHint"`
		DestructiveHint *bool  `json:"destructiveHint"`
		IdempotentHint  *bool  `json:"idempotentHint"`
		OpenWorldHint   *bool  `json:"openWorldHint"`
	} `json:"annotations"`
}

func parseToolPages(pages [][]byte) ([]*rawTool, error) {
	var tools []*rawTool
	for _, page := range pages {
		var list []json.RawMessage
		found := false
		for m := range messages(page) {
			var msg struct {
				Result *struct {
					Tools []json.RawMessage `json:"tools"`
				} `json:"result"`
			}
			if json.Unmarshal(m, &msg) == nil && msg.Result != nil {
				list, found = msg.Result.Tools, true
				break
			}
		}
		if !found {
			return tools, errors.New("a tools/list answer held no result")
		}
		for _, raw := range list {
			var wt wireTool
			if json.Unmarshal(raw, &wt) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				continue
			}
			t := &rawTool{name: wt.Name, title: wt.Title, annotTitle: wt.Annotations.Title, description: wt.Description,
				hints: map[string]bool{}, input: wt.InputSchema}
			if len(wt.OutputSchema) > 0 && string(wt.OutputSchema) != "null" {
				t.output = wt.OutputSchema
			}
			for i, h := range []*bool{wt.Annotations.ReadOnlyHint, wt.Annotations.DestructiveHint, wt.Annotations.IdempotentHint, wt.Annotations.OpenWorldHint} {
				if h != nil {
					t.hints[hintNames[i]] = *h
				}
			}
			tools = append(tools, t)
			if len(tools) == maxTools {
				return tools, nil
			}
		}
	}
	return tools, nil
}
