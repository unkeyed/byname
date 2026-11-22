// Package app is the set of services REST and MCP both call, so the two
// transports serve the same operations.
package app

import (
	"github.com/unkeyed/byname/internal/changes"
	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/gsc"
	"github.com/unkeyed/byname/internal/instance"
	"github.com/unkeyed/byname/internal/logos"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/social"
)

type Services struct {
	Instance  *instance.Service
	Config    *config.Service
	Checks    *checks.Service
	Results   *results.Service
	Notify    *notify.Service
	GSC       *gsc.Service
	Changes   *changes.Service
	Social    *social.Service
	Discovery *discovery.Service
	Logos     *logos.Service
}
