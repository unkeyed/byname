// Package app is the set of services REST and MCP both call, so the two
// transports serve the same operations.
package app

import (
	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/gsc"
	"github.com/unkeyed/byname/internal/instance"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
)

type Services struct {
	Instance *instance.Service
	Config   *config.Service
	Checks   *checks.Service
	Results  *results.Service
	Notify   *notify.Service
	GSC      *gsc.Service
}
