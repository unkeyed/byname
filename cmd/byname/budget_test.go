package main

import "testing"

func TestBudgetLeavesRoomOnTheDatabase(t *testing.T) {
	for _, c := range []struct {
		name                string
		configured, allowed int32
		want                int32
	}{
		{"a large database keeps the default", 20, 97, 20},
		{"a small one gets a third of its slots", 20, 45, 15},
		{"a tiny one still gets the fewest openLanes can split", 20, 12, minPoolSize},
		{"a lower setting is kept", 10, 97, 10},
	} {
		if got := budget(c.configured, c.allowed); got != c.want {
			t.Errorf("%s: budget(%d, %d) = %d, want %d", c.name, c.configured, c.allowed, got, c.want)
		}
	}
}
