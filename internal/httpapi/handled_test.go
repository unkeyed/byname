package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/unkeyed/byname/internal/core"
)

// A reader lists marked pages and the answers that link to them; only
// config:write marks a question as handled or removes the mark.
func TestHandledOverHTTP(t *testing.T) {
	c := newServer(t)
	person, agent := login(t, c)
	reader := c.withKey(t, person, core.ResultsRead)
	configure(t, person, agent)
	c.treg.SetBody("dataforseo.x.ai-optimization-claude-llm-responses-live", []byte(`{"status_code":20000,"tasks":[{"status_code":20000,"result":[{"model_name":"claude-sonnet-4-5-20250929",
		"items":[{"type":"message","sections":[{"type":"text","text":"Contoso Gateway is the usual choice.",
		"annotations":[{"title":"Zero downtime deploys","url":"https://www.northwind.example/blog/zero-downtime/"}]}]}]}]}]}`))
	check := func() {
		t.Helper()
		agent.call("POST", "/v1/runs", nil, http.StatusCreated, nil)
		if err := c.svc.Checks.Work(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var set struct {
		Questions []struct {
			ID int64 `json:"id"`
		} `json:"questions"`
	}
	reader.call("GET", "/v1/questions", nil, http.StatusOK, &set)
	q := set.Questions[0].ID
	mark := fmt.Sprintf("/v1/questions/%d/pages", q)
	page := map[string]string{"url": "northwind.example/blog/zero-downtime"}

	reader.call("POST", mark, page, http.StatusForbidden, nil)
	agent.call("POST", mark, map[string]string{"url": "mailto:hi@northwind.example"}, http.StatusBadRequest, nil)
	agent.call("POST", fmt.Sprintf("/v1/questions/%d/pages", q+100), page, http.StatusNotFound, nil)
	var marked struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	agent.call("POST", mark, page, http.StatusOK, &marked)
	if marked.URL != "https://northwind.example/blog/zero-downtime" {
		t.Errorf("marked url = %q, want https:// added", marked.URL)
	}
	check()

	var list struct {
		Pages []struct {
			ID       int64 `json:"id"`
			Question struct {
				ID int64 `json:"id"`
			} `json:"question"`
			Linked struct {
				Answers int `json:"answers"`
			} `json:"linked"`
			Now *struct {
				Answered int `json:"answered"`
			} `json:"now"`
		} `json:"pages"`
	}
	reader.call("GET", "/v1/handled", nil, http.StatusOK, &list)
	if len(list.Pages) != 1 || list.Pages[0].ID != marked.ID || list.Pages[0].Question.ID != q || list.Pages[0].Linked.Answers != 2 || list.Pages[0].Now == nil {
		t.Fatalf("handled = %+v; want the page, linked by Claude Code's answer to each question, with the check since", list.Pages)
	}
	var gaps struct {
		Gaps []struct {
			Question struct {
				ID int64 `json:"id"`
			} `json:"question"`
			Pages []struct {
				ID     int64 `json:"id"`
				Linked int   `json:"linked"`
			} `json:"pages"`
		} `json:"gaps"`
	}
	reader.call("GET", "/v1/gaps", nil, http.StatusOK, &gaps)
	carried := false
	for _, g := range gaps.Gaps {
		if g.Question.ID == q {
			carried = len(g.Pages) == 1 && g.Pages[0].ID == marked.ID && g.Pages[0].Linked == 2
		}
	}
	if !carried {
		t.Errorf("gaps = %+v; want the question's row to carry its page", gaps.Gaps)
	}

	unmark := fmt.Sprintf("/v1/question-pages/%d", marked.ID)
	reader.call("DELETE", unmark, nil, http.StatusForbidden, nil)
	agent.call("DELETE", unmark, nil, http.StatusNoContent, nil)
	agent.call("DELETE", unmark, nil, http.StatusNotFound, nil)
	reader.call("GET", "/v1/handled", nil, http.StatusOK, &list)
	if len(list.Pages) != 0 {
		t.Errorf("after removing, handled = %+v", list.Pages)
	}
}
