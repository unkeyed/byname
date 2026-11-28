package mcpgrade

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recorder answers every request and remembers the Authorization header
// each path saw.
type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	auth map[string][]string
}

func newRecorder(t *testing.T, h http.HandlerFunc) *recorder {
	t.Helper()
	rec := &recorder{auth: map[string][]string{}}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auth[r.URL.Path] = append(rec.auth[r.URL.Path], r.Header.Get("Authorization"))
		rec.mu.Unlock()
		if h != nil {
			h(w, r)
		}
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (rec *recorder) saw(path string) []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.auth[path]
}

func get(t *testing.T, c *http.Client, target string, header http.Header) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	resp.Body.Close()
}

func TestWireSendsTokenOnlyToEndpoint(t *testing.T) {
	other := newRecorder(t, nil)
	target := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" && r.URL.Query().Has("away") {
			http.Redirect(w, r, other.URL+"/landing", http.StatusFound)
		}
	})
	w := newWire(Target{URL: target.URL + "/mcp", Token: "secret", Client: http.DefaultClient})
	authd, bare := w.client(http.DefaultClient, true), w.client(http.DefaultClient, false)

	get(t, authd, target.URL+"/mcp", nil)
	get(t, bare, target.URL+"/mcp", nil)
	get(t, authd, target.URL+"/.well-known/oauth-protected-resource/mcp", nil)
	get(t, authd, other.URL+"/mcp", http.Header{"Authorization": {"Bearer smuggled"}})
	get(t, authd, target.URL+"/mcp?away=1", nil)

	if got := target.saw("/mcp"); len(got) != 3 || got[0] != "Bearer secret" || got[1] != "" || got[2] != "" {
		t.Errorf("the endpoint saw %q, want the token only from the token client on the exact URL", got)
	}
	if got := target.saw("/.well-known/oauth-protected-resource/mcp"); len(got) != 1 || got[0] != "" {
		t.Errorf("a well-known document on the endpoint's origin saw %q", got)
	}
	if got := other.saw("/mcp"); len(got) != 1 || got[0] != "" {
		t.Errorf("another server saw %q, want no Authorization even when the request set one", got)
	}
	if got := other.saw("/landing"); len(got) != 1 || got[0] != "" {
		t.Errorf("a redirect target saw %q", got)
	}
}

func TestWireCapsBodiesAndRequests(t *testing.T) {
	endless := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := strings.Repeat(": keepalive\n\n", 1000)
		for r.Context().Err() == nil {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
	})
	w := newWire(Target{URL: endless.URL, Client: http.DefaultClient})
	c := w.client(http.DefaultClient, false)
	read := func() (int, error) {
		resp, err := c.Get(endless.URL)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return len(b), err
	}

	for i := range 4 {
		if n, err := read(); !errors.Is(err, errBodyCap) || n != bodyCap {
			t.Fatalf("endless body %d read %d bytes with %v, want %d and the body cap", i, n, err, bodyCap)
		}
	}
	if _, err := read(); !errors.Is(err, errGradeCap) {
		t.Fatalf("a fifth endless body read with %v, want the grade's byte budget spent", err)
	}
	ok := 5
	for ; ok < maxRequests+5; ok++ {
		resp, err := c.Get(endless.URL + "/nothing")
		if errors.Is(err, errRequests) {
			break
		}
		if err == nil {
			resp.Body.Close()
		}
	}
	if ok != maxRequests {
		t.Errorf("%d requests went out, want %d", ok, maxRequests)
	}
}

func TestParseToolPages(t *testing.T) {
	sse := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n" +
		"id: 2\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\n" +
		"data: \"result\":{\"tools\":[null,{\"name\":\"b\",\"annotations\":{\"readOnlyHint\":false}}]}}\n\n"
	json := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a","outputSchema":null}],"nextCursor":"2"}}`
	tools, err := parseToolPages([][]byte{[]byte(json), []byte(sse)})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].name != "a" || tools[0].output != nil || tools[1].name != "b" {
		t.Fatalf("tools %+v", tools)
	}
	if v, declared := tools[1].hints["read_only"]; !declared || v {
		t.Errorf("readOnlyHint false should read as declared false, got %v %v", v, declared)
	}
	if _, declared := tools[1].hints["destructive"]; declared {
		t.Error("an absent destructiveHint should read as not declared")
	}
	if _, err := parseToolPages([][]byte{[]byte(": keepalive\n\n")}); err == nil {
		t.Error("a page with no result should be an error")
	}
}
