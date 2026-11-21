// Package instance answers questions about this Byname instance as a
// whole. REST and MCP both call it, so an agent sees what the UI sees.
package instance

import "context"

// Status is what GET /v1/status and the status tool return.
type Status struct {
	Version string `json:"version"`
	// Onboarded is whether someone has finished onboarding. Until then
	// nothing runs on schedule and Run now refuses.
	Onboarded bool `json:"onboarded"`
}

// Onboarding reports whether onboarding is finished. *config.Service
// implements it.
type Onboarding interface {
	Onboarded(context.Context) (bool, error)
}

type Service struct {
	version    string
	onboarding Onboarding
}

func New(version string, o Onboarding) *Service { return &Service{version: version, onboarding: o} }

func (s *Service) Status(ctx context.Context) (Status, error) {
	done, err := s.onboarding.Onboarded(ctx)
	return Status{Version: s.version, Onboarded: done}, err
}
