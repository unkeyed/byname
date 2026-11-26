package notify

import (
	"encoding/json"
	"os"
	"testing"
)

func TestChangeLineMatchesSharedCases(t *testing.T) {
	raw, err := os.ReadFile("changes.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		PreviousRunID       int64 `json:"previous_run_id"`
		Gained, Lost, Moved int
		Text                string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("changes.json has no cases")
	}
	for _, c := range cases {
		if got := changeLine(c.PreviousRunID, c.Gained, c.Lost, c.Moved); got != c.Text {
			t.Errorf("changeLine(%d, %d, %d, %d) = %q, want %q", c.PreviousRunID, c.Gained, c.Lost, c.Moved, got, c.Text)
		}
	}
}
