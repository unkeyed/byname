// Package instance answers questions about this Byname instance as a
// whole. REST and MCP both call it, so an agent sees what the UI sees.
package instance

import "context"

// Status is what GET /v1/status and the status tool return.
type Status struct {
	Version string `json:"version"`
	// Onboarded is whether someone has finished setting up the brand,
	// surfaces and questions. Until then there is nothing to check.
	Onboarded bool `json:"onboarded"`
}

type Service struct {
	version string
}

func New(version string) *Service { return &Service{version: version} }

// Status reports Onboarded as false: no onboarding step exists to finish.
func (s *Service) Status(context.Context) (Status, error) {
	return Status{Version: s.version}, nil
}
