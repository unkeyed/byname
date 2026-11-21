// Package githubtest is a fake of the GitHub REST API calls site changes
// makes: GET /repos/{owner}/{repo}, the closed pull request list sorted by
// update, and a pull request's files. Response shapes follow GitHub's REST
// reference (2022-11-28), trimmed to the fields Byname reads, including
// the GitHub-Authentication-Token-Expiration header fine-grained tokens
// get on every response.
package githubtest

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Pull is a pull request into the repo's default branch.
type Pull struct {
	Number   int
	Title    string
	MergedAt *time.Time // nil for one closed without merging
	Updated  time.Time
	Files    []File
}

// File is one file a pull request changed, with GitHub's status: added,
// modified, removed or renamed.
type File struct {
	Name   string
	Status string
}

// Fake is the fake GitHub. Lock it to change it while serving.
type Fake struct {
	mu      sync.Mutex
	Token   string
	Expires *time.Time
	Owner   string
	Repo    string
	Branch  string
	pulls   []Pull
	calls   []string
}

func NewFake(owner, repo, token string) *Fake {
	return &Fake{Owner: owner, Repo: repo, Branch: "main", Token: token}
}

// AddPull adds or replaces a pull request.
func (f *Fake) AddPull(p Pull) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulls = slices.DeleteFunc(f.pulls, func(q Pull) bool { return q.Number == p.Number })
	f.pulls = append(f.pulls, p)
}

// SetExpiry sets the token expiry the fake reports, nil for none.
func (f *Fake) SetExpiry(t *time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Expires = t
}

// Calls is every request path the fake served, in order.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Handler serves the fake.
func (f *Fake) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{repo}", f.authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"full_name": f.Owner + "/" + f.Repo, "default_branch": f.Branch, "private": true})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/pulls", f.authed(f.list))
	mux.HandleFunc("GET /repos/{owner}/{repo}/pulls/{number}/files", f.authed(f.files))
	return mux
}

func (f *Fake) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer "+f.Token {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]string{"message": "Bad credentials", "documentation_url": "https://docs.github.com/rest"})
			return
		}
		if f.Expires != nil {
			w.Header().Set("GitHub-Authentication-Token-Expiration", f.Expires.UTC().Format("2006-01-02 15:04:05 MST"))
		}
		if r.PathValue("owner") != f.Owner || r.PathValue("repo") != f.Repo {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"message": "Not Found"})
			return
		}
		next(w, r)
	}
}

func page(r *http.Request) (int, int) {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	return max(n, 1), cmp.Or(min(per, 100), 30)
}

func (f *Fake) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("state") != "closed" || q.Get("sort") != "updated" || q.Get("direction") != "desc" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		writeJSON(w, map[string]string{"message": "the fake serves state=closed&sort=updated&direction=desc only"})
		return
	}
	pulls := slices.Clone(f.pulls)
	if base := q.Get("base"); base != "" && base != f.Branch {
		pulls = nil
	}
	slices.SortFunc(pulls, func(a, b Pull) int { return b.Updated.Compare(a.Updated) })
	n, per := page(r)
	out := []map[string]any{}
	for i := (n - 1) * per; i < len(pulls) && i < n*per; i++ {
		p := pulls[i]
		out = append(out, map[string]any{
			"number": p.Number, "title": p.Title, "state": "closed", "merged_at": p.MergedAt, "updated_at": p.Updated,
			"html_url": fmt.Sprintf("https://github.com/%s/%s/pull/%d", f.Owner, f.Repo, p.Number),
			"base":     map[string]string{"ref": f.Branch},
		})
	}
	writeJSON(w, out)
}

func (f *Fake) files(w http.ResponseWriter, r *http.Request) {
	num, _ := strconv.Atoi(r.PathValue("number"))
	i := slices.IndexFunc(f.pulls, func(p Pull) bool { return p.Number == num })
	if i < 0 {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"message": "Not Found"})
		return
	}
	n, per := page(r)
	out := []map[string]any{}
	files := f.pulls[i].Files
	for j := (n - 1) * per; j < len(files) && j < n*per; j++ {
		out = append(out, map[string]any{"filename": files[j].Name, "status": files[j].Status, "additions": 1, "deletions": 0, "changes": 1})
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// Server is a running Fake.
type Server struct {
	*Fake
	URL string
}

// New starts a fake GitHub for one test.
func New(t testing.TB, owner, repo, token string) *Server {
	t.Helper()
	f := NewFake(owner, repo, token)
	srv := httptest.NewServer(f.Handler())
	t.Cleanup(srv.Close)
	return &Server{Fake: f, URL: srv.URL}
}
