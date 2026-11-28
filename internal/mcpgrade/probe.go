package mcpgrade

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type observation struct {
	endpoint   string
	tokenGiven bool

	bareInit  httpSeen
	legacySSE bool
	auth      *authSeen

	session    *sessionSeen
	sessionErr error

	html     httpSeen
	manifest manifestSeen
	registry registrySeen
}

type httpSeen struct {
	status      int
	contentType string
	header      http.Header
	err         error
}

type authSeen struct {
	challenges   []oauthex.Challenge
	challengeErr error
	prmLookedAt  string
	prm          *oauthex.ProtectedResourceMetadata
	prmErr       error
	asURL        string
	as           *oauthex.AuthServerMeta
	asErr        error
}

type sessionSeen struct {
	init       *mcp.InitializeResult
	negotiate  time.Duration
	tools      []*rawTool
	toolsErr   error
	lists      map[string]error
	badVersion httpSeen
	unknownRPC rpcSeen
	origin     httpSeen
	noOrigin   httpSeen
}

type rpcSeen struct {
	httpSeen
	code int64
}

type manifestSeen struct {
	at            string
	namesEndpoint bool
}

type registrySeen struct {
	listed bool
	err    error
}

// rawTool is one tool as tools/list sent it, not as the SDK decoded it: the
// SDK drops tools it finds invalid and reads an undeclared readOnlyHint as
// false.
type rawTool struct {
	name, title, annotTitle, description string
	hints                                map[string]bool
	input                                json.RawMessage
	output                               json.RawMessage
}

var hintNames = []string{"read_only", "destructive", "idempotent", "open_world"}

const (
	stepTimeout = 10 * time.Second
	clientName  = "byname-grader"
	probeOrigin = "https://byname-probe.invalid"
)

func probe(ctx context.Context, t Target) *observation {
	o := &observation{endpoint: t.URL, tokenGiven: t.Token != ""}
	w := newWire(t)
	bare, authd := w.client(t.Client, false), w.client(t.Client, true)
	registry := strings.TrimSuffix(cmp.Or(t.RegistryURL, defaultRegistry), "/")

	var wg sync.WaitGroup
	wg.Go(func() {
		o.html, _ = exchange(ctx, bare, http.MethodGet, t.URL, nil, http.Header{"Accept": {"text/html"}}, 0)
		o.manifest = findManifest(ctx, bare, t.URL)
		o.registry = lookupRegistry(ctx, bare, registry, t.URL)
	})

	o.bareInit = firstPost(ctx, bare, t.URL)
	if o.asksSignIn() {
		o.auth = discoverAuth(ctx, bare, t.URL, o.bareInit.header)
	}
	if !needsToken(o) {
		o.session, o.sessionErr = connect(ctx, authd, w, t.URL)
		if o.session == nil && !o.asksSignIn() && !o.bareInit.ok() {
			o.legacySSE = legacySSE(ctx, authd, t.URL)
		}
	}
	wg.Wait()
	return o
}

func exchange(ctx context.Context, c *http.Client, method, target string, body []byte, header http.Header, read int64) (httpSeen, []byte) {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return httpSeen{err: err}, nil
	}
	maps.Copy(req.Header, header)
	resp, err := c.Do(req)
	if err != nil {
		return httpSeen{err: err}, nil
	}
	defer resp.Body.Close()
	h := httpSeen{status: resp.StatusCode, contentType: resp.Header.Get("Content-Type"), header: resp.Header}
	var b []byte
	if read > 0 {
		b, _ = io.ReadAll(io.LimitReader(resp.Body, read))
	}
	return h, b
}

// From 2026-07-28 a server refuses a request whose Mcp-Method header is
// missing or differs from the body's method; earlier ones ignore it.
func rpcHeader(method, session, version string) http.Header {
	h := http.Header{
		"Content-Type": {"application/json"},
		"Accept":       {"application/json, text/event-stream"},
		"Mcp-Method":   {method},
	}
	if version != "" {
		h.Set("MCP-Protocol-Version", version)
	}
	if session != "" {
		h.Set("Mcp-Session-Id", session)
	}
	return h
}

// From 2026-07-28 every request carries the protocol version and client
// capabilities in _meta.
func rpc(method, version string) []byte {
	params := map[string]any{}
	if version >= "2026-07-28" {
		params["_meta"] = map[string]any{
			mcp.MetaKeyProtocolVersion:    version,
			mcp.MetaKeyClientInfo:         map[string]string{"name": clientName, "version": "1"},
			mcp.MetaKeyClientCapabilities: map[string]any{},
		}
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	return b
}

func firstPost(ctx context.Context, c *http.Client, endpoint string) httpSeen {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": clientName, "version": "1"},
	}})
	h, _ := exchange(ctx, c, http.MethodPost, endpoint, body, rpcHeader("initialize", "", ""), 0)
	if id := h.header.Get("Mcp-Session-Id"); id != "" {
		del := http.Header{"Mcp-Session-Id": {id}, "MCP-Protocol-Version": {"2025-11-25"}}
		exchange(ctx, c, http.MethodDelete, endpoint, nil, del, 0)
	}
	return h
}

// discoverAuth fetches the documents itself rather than through oauthex's
// getters, which refuse a document with a wrong resource or no PKCE without
// saying what it held.
func discoverAuth(ctx context.Context, c *http.Client, endpoint string, h http.Header) *authSeen {
	a := &authSeen{}
	a.challenges, a.challengeErr = oauthex.ParseWWWAuthenticate(h.Values("WWW-Authenticate"))
	var prmURLs []string
	for _, ch := range a.challenges {
		if u := ch.Params["resource_metadata"]; ch.Scheme == "bearer" && u != "" {
			prmURLs = append(prmURLs, u)
		}
	}
	prmURLs = append(prmURLs, wellKnown(endpoint, "oauth-protected-resource")...)
	for _, u := range slices.Compact(prmURLs) {
		a.prmLookedAt = u
		if a.prm, a.prmErr = getJSON[oauthex.ProtectedResourceMetadata](ctx, c, u); a.prm != nil {
			break
		}
	}
	if a.prm == nil || len(a.prm.AuthorizationServers) == 0 {
		return a
	}
	a.asURL = a.prm.AuthorizationServers[0]
	urls := append(wellKnown(a.asURL, "oauth-authorization-server"), wellKnown(a.asURL, "openid-configuration")...)
	urls = append(urls, strings.TrimSuffix(a.asURL, "/")+"/.well-known/openid-configuration")
	for _, u := range slices.Compact(urls) {
		if a.as, a.asErr = getJSON[oauthex.AuthServerMeta](ctx, c, u); a.as != nil {
			break
		}
	}
	return a
}

func wellKnown(target, name string) []string {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return nil
	}
	root := u.Scheme + "://" + u.Host + "/.well-known/" + name
	if p := strings.TrimSuffix(u.Path, "/"); p != "" {
		return []string{root + p, root}
	}
	return []string{root}
}

func getJSON[T any](ctx context.Context, c *http.Client, target string) (*T, error) {
	h, b := exchange(ctx, c, http.MethodGet, target, nil, http.Header{"Accept": {"application/json"}}, 1<<20)
	switch {
	case h.err != nil:
		return nil, h.err
	case h.status != http.StatusOK:
		return nil, fmt.Errorf("HTTP %d", h.status)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("not JSON: %w", err)
	}
	return &v, nil
}

func connect(ctx context.Context, c *http.Client, w *wire, endpoint string) (*sessionSeen, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: clientName, Version: "1"}, &mcp.ClientOptions{
		Logger:       slog.New(slog.DiscardHandler),
		Capabilities: &mcp.ClientCapabilities{},
	})
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: c, MaxRetries: -1, DisableStandaloneSSE: true}
	start := time.Now()
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	s := &sessionSeen{init: cs.InitializeResult(), negotiate: time.Since(start), lists: map[string]error{}}

	listed := 0
	for _, err := range cs.Tools(ctx, nil) {
		if err != nil {
			s.toolsErr = err
			break
		}
		if listed++; listed == maxTools {
			break
		}
	}
	pages, cut := w.pages()
	s.tools, err = parseToolPages(pages)
	s.toolsErr = cmp.Or(cut, s.toolsErr, err)

	if caps := s.init.Capabilities; caps != nil {
		if caps.Resources != nil {
			_, s.lists["resources/list"] = cs.ListResources(ctx, nil)
		}
		if caps.Prompts != nil {
			_, s.lists["prompts/list"] = cs.ListPrompts(ctx, nil)
		}
	}

	v, id := s.init.ProtocolVersion, cs.ID()
	// A request every server of the version answers: 2026-07-28 drops ping.
	benign := "ping"
	if v >= "2026-07-28" {
		benign = "server/discover"
	}
	s.badVersion, _ = exchange(ctx, c, http.MethodPost, endpoint, rpc(benign, ""), rpcHeader(benign, id, "1999-01-01"), 0)
	h, body := exchange(ctx, c, http.MethodPost, endpoint, rpc("byname/unknown", v), rpcHeader("byname/unknown", id, v), 64<<10)
	s.unknownRPC = rpcSeen{httpSeen: h, code: errorCode(body)}
	s.noOrigin, _ = exchange(ctx, c, http.MethodPost, endpoint, rpc(benign, v), rpcHeader(benign, id, v), 0)
	header := rpcHeader(benign, id, v)
	header.Set("Origin", probeOrigin)
	s.origin, _ = exchange(ctx, c, http.MethodPost, endpoint, rpc(benign, v), header, 0)
	return s, nil
}

func legacySSE(ctx context.Context, c *http.Client, endpoint string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return false
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 64<<10))
	for sc.Scan() {
		if ev, ok := strings.CutPrefix(sc.Text(), "event:"); ok && strings.TrimSpace(ev) == "endpoint" {
			return true
		}
	}
	return false
}

func findManifest(ctx context.Context, c *http.Client, endpoint string) manifestSeen {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return manifestSeen{}
	}
	for _, path := range []string{"/.well-known/mcp.json", "/mcp.json"} {
		at := u.Scheme + "://" + u.Host + path
		h, b := exchange(ctx, c, http.MethodGet, at, nil, http.Header{"Accept": {"application/json"}}, 1<<20)
		var v any
		if h.status != http.StatusOK || json.Unmarshal(b, &v) != nil {
			continue
		}
		m := manifestSeen{at: at}
		for s := range stringsIn(v) {
			if sameURL(s, endpoint) {
				m.namesEndpoint = true
				break
			}
		}
		return m
	}
	return manifestSeen{}
}

func lookupRegistry(ctx context.Context, c *http.Client, registry, endpoint string) registrySeen {
	var lastErr error
	reached := false
	for _, term := range registryTerms(endpoint) {
		h, b := exchange(ctx, c, http.MethodGet, registry+"/v0/servers?limit=100&search="+url.QueryEscape(term),
			nil, http.Header{"Accept": {"application/json"}}, bodyCap)
		if h.err != nil {
			lastErr = h.err
			continue
		}
		var res struct {
			Servers []struct {
				Server struct {
					Remotes []struct {
						URL string `json:"url"`
					} `json:"remotes"`
				} `json:"server"`
			} `json:"servers"`
		}
		if h.status != http.StatusOK || json.Unmarshal(b, &res) != nil {
			lastErr = fmt.Errorf("registry answered HTTP %d %s", h.status, h.contentType)
			continue
		}
		reached = true
		for _, s := range res.Servers {
			for _, r := range s.Server.Remotes {
				if sameURL(r.URL, endpoint) {
					return registrySeen{listed: true}
				}
			}
		}
	}
	if !reached {
		return registrySeen{err: lastErr}
	}
	return registrySeen{}
}

func registryTerms(endpoint string) []string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil
	}
	labels := strings.Split(strings.ToLower(u.Hostname()), ".")
	if len(labels) < 2 {
		return []string{strings.Join(labels, ".")}
	}
	sld, tld := labels[len(labels)-2], labels[len(labels)-1]
	return []string{tld + "." + sld, sld}
}
