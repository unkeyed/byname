package mcpgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

func score(o *observation) Report {
	var tools []*rawTool
	if o.session != nil {
		tools = o.session.tools
	}
	r := Report{Rules: rules, Server: serverInfo(o), Tools: make([]Tool, len(tools))}
	for i, t := range tools {
		r.Tools[i] = describe(t)
	}

	byCategory := map[string][]Check{}
	docEarned := make([]float64, len(tools))
	docWeight := make([]float64, len(tools))
	for _, c := range checks {
		var m mark
		switch e := c.eval.(type) {
		case serverCheck:
			m = e(o)
		case toolCheck:
			var marks []mark
			m, marks = judgeTools(o, tools, e)
			for i, tm := range marks {
				r.Tools[i].Marks = append(r.Tools[i].Marks, ToolMark{Check: c.id, Status: status(tm), Finding: tm.finding})
				if c.category() == "documentation" && !tm.skipped {
					docEarned[i] += float64(c.weight) * tm.fraction
					docWeight[i] += float64(c.weight)
				}
			}
		}
		res := Check{ID: c.id, Title: c.title, Weight: c.weight, Status: status(m), Finding: m.finding}
		if !m.skipped {
			res.Fraction = m.fraction
		}
		if res.Status != Pass && res.Status != Skipped {
			res.Fix = c.fix
		}
		byCategory[c.category()] = append(byCategory[c.category()], res)
	}
	for i := range r.Tools {
		if docWeight[i] > 0 {
			r.Tools[i].Score = int(math.Round(100 * docEarned[i] / docWeight[i]))
		}
	}

	var sum, counted int
	for _, cat := range categories {
		c := Category{ID: cat.id, Title: cat.title, Checks: byCategory[cat.id]}
		var earned, weight float64
		for _, ch := range c.Checks {
			if ch.Status == Skipped {
				continue
			}
			earned += float64(ch.Weight) * ch.Fraction
			weight += float64(ch.Weight)
		}
		switch {
		case weight > 0:
			c.Score = new(int(math.Round(100 * earned / weight)))
		case cat.required:
			c.Score = new(0)
			c.Note = c.Checks[0].Finding
			r.Partial = partialReason(o)
		default:
			c.Note = c.Checks[0].Finding
		}
		if c.Score != nil {
			sum += *c.Score
			counted++
		}
		r.Categories = append(r.Categories, c)
	}
	r.Score = int(math.Round(float64(sum) / float64(counted)))
	r.Grade = letter(r.Score)
	return r
}

// partialReason follows the notes the skipped checks carry: without a
// session the tool checks say noSession(o), and with one the only required
// category that can go unassessed is documentation, when no tool is listed.
func partialReason(o *observation) PartialReason {
	switch {
	case needsToken(o):
		return NeedsToken
	case o.session == nil:
		return NoSession
	}
	return NoTools
}

const maxNamed = 5

func status(m mark) Status {
	switch {
	case m.skipped:
		return Skipped
	case m.fraction >= 1:
		return Pass
	case m.fraction <= 0:
		return Fail
	}
	return Partial
}

func judgeTools(o *observation, tools []*rawTool, judge toolCheck) (mark, []mark) {
	if o.session == nil {
		return skip(noSession(o)), nil
	}
	if len(tools) == 0 {
		return skip("No tools listed"), nil
	}
	marks := make([]mark, len(tools))
	var sum float64
	var judged int
	var short []string
	for i, t := range tools {
		marks[i] = judge(t)
		if marks[i].skipped {
			continue
		}
		judged++
		sum += marks[i].fraction
		if marks[i].fraction < 1 {
			short = append(short, fmt.Sprintf("%s (%s)", t.name, marks[i].finding))
		}
	}
	if judged == 0 {
		return skip(marks[0].finding), marks
	}
	f := sum / float64(judged)
	if len(short) == 0 {
		return pass(fmt.Sprintf("All %d tools pass", judged)), marks
	}
	finding := fmt.Sprintf("%d of %d tools fall short: %s", len(short), judged, strings.Join(short[:min(maxNamed, len(short))], "; "))
	if len(short) > maxNamed {
		finding += fmt.Sprintf("; and %d more", len(short)-maxNamed)
	}
	return mark{fraction: f, finding: finding}, marks
}

func serverInfo(o *observation) ServerInfo {
	s := ServerInfo{Transport: transportOf(o)}
	if o.session == nil {
		return s
	}
	s.ProtocolVersion = o.session.init.ProtocolVersion
	s.NegotiateMS = new(int(o.session.negotiate.Milliseconds()))
	s.TokenUsed = o.tokenGiven
	if si := o.session.init.ServerInfo; si != nil {
		s.Name, s.Title, s.Version = si.Name, si.Title, si.Version
	}
	return s
}

const maxDescription = 8 << 10

func describe(t *rawTool) Tool {
	d := t.description
	if len(d) > maxDescription {
		d = strings.ToValidUTF8(d[:maxDescription], "")
	}
	out := Tool{Name: t.name, Title: t.displayTitle(), Description: d, Params: []Param{}, Hints: []string{}, HasOutput: t.output != nil}
	for _, p := range params(t.input) {
		out.Params = append(out.Params, p.Param)
	}
	for _, h := range hintNames {
		if t.hints[h] {
			out.Hints = append(out.Hints, h)
		}
	}
	def, _ := json.Marshal(struct {
		Name, Title, Description string
		Params                   []Param
		Hints                    []string
		HasOutput                bool
	}{out.Name, out.Title, out.Description, out.Params, out.Hints, out.HasOutput})
	sum := sha256.Sum256(def)
	out.Key = hex.EncodeToString(sum[:])
	return out
}
