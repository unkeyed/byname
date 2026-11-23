package main

import "testing"

func TestBudgetLeavesRoomOnTheDatabase(t *testing.T) {
	for _, c := range []struct {
		name                string
		configured, allowed int32
		want                int32
	}{
		{"a large database keeps the default", 20, 97, 20},
		{"a small one gets a quarter of its slots", 20, 45, 11},
		{"PlanetScale's 25, less 3 reserved, gets 5", 20, 22, 5},
		{"a tiny one still gets the fewest openLanes can split", 20, 12, minPoolSize},
		{"a lower setting is kept", 10, 97, 10},
	} {
		if got := budget(c.configured, c.allowed); got != c.want {
			t.Errorf("%s: budget(%d, %d) = %d, want %d", c.name, c.configured, c.allowed, got, c.want)
		}
	}
}

func TestSplitFillsTheBudgetExactly(t *testing.T) {
	for _, total := range []int32{minPoolSize, 6, 8, 12, 20, 40} {
		web, jobs, locks := split(total)
		if web+jobs+locks != total || web < 2 || jobs < 2 || locks < 1 || locks > 4 {
			t.Errorf("split(%d) = web %d, jobs %d, locks %d; want them to add up, with at least 2, 2 and 1", total, web, jobs, locks)
		}
	}
}
