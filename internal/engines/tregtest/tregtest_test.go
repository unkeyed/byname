package tregtest

import (
	"testing"

	"github.com/unkeyed/byname/internal/engines"
)

// TestEveryEndpointIsServed fails when a pinned endpoint or model list has
// no body here, so tests can enable any surface.
func TestEveryEndpointIsServed(t *testing.T) {
	New(t)
	for _, s := range engines.All() {
		if _, ok := Fixtures[s.Endpoint.ID]; !ok {
			t.Errorf("%s: no fixture for %s", s.Surface, s.Endpoint.ID)
		}
		if _, ok := Fixtures[s.Endpoint.Models]; s.Endpoint.TakesModel() && !ok {
			t.Errorf("%s: no fixture for %s", s.Surface, s.Endpoint.Models)
		}
	}
}
