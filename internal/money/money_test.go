package money_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/unkeyed/byname/internal/money"
)

func TestFormatMatchesSharedCases(t *testing.T) {
	raw, err := os.ReadFile("cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		USD  float64 `json:"usd"`
		Text string  `json:"text"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("cases.json has no cases")
	}
	for _, c := range cases {
		if got := money.Format(c.USD); got != c.Text {
			t.Errorf("Format(%v) = %q, want %q", c.USD, got, c.Text)
		}
	}
}
