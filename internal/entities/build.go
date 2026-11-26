// Package entities keeps every entity an instance tracks live: its
// services, bound to its own tables, and its scheduler and notification
// delivery, each running once per process.
package entities

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/changes"
	"github.com/unkeyed/byname/internal/checks"
	"github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/instance"
	"github.com/unkeyed/byname/internal/logos"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/scheduler"
	"github.com/unkeyed/byname/internal/search"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/social"
	"github.com/unkeyed/byname/internal/sources"
)

// Options is what every entity's services are built with. The base URL
// overrides are for tests and local development against fakes; empty means
// the real service.
type Options struct {
	Version   string
	Secrets   *secretbox.Box
	PublicURL *url.URL
	Log       *slog.Logger
	// MigrateDB, when set, is the role that creates a new entity's
	// tables: MIGRATION_DATABASE_URL's.
	MigrateDB *pgxpool.Config
	// Resolve follows redirect links in citations; nil keeps them as sent.
	Resolve       engines.Resolver
	TregBaseURL   string
	ProviderURLs  map[engines.CredentialKind]string
	ResendBaseURL string
	GSCClient     search.OAuthClient
	GSCBaseURL    string
	BingBaseURL   string
	GitHubBaseURL string
	// BlueskyBaseURL replaces Bluesky's search API.
	BlueskyBaseURL string
	// SourcesAllowPrivate lets cited-source fetching and logo downloads
	// reach private and loopback addresses.
	SourcesAllowPrivate bool
}

// Build makes the services of the entity pool is bound to, and run,
// which runs that entity's scheduler and notification delivery until
// ctx ends. It is the only place an entity's services are put together.
func Build(w db.Entity, pool db.Pool, o Options) (app.Services, func(context.Context)) {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("entity", w.Slug)
	res := results.New(pool)
	alerts := notify.New(pool, res, notify.Options{
		Secrets: o.Secrets, PublicURL: o.PublicURL, HTTPClient: &http.Client{}, ResendBaseURL: o.ResendBaseURL, Log: log,
	})
	settings := config.New(pool, config.Options{
		Secrets: o.Secrets, TregBaseURL: o.TregBaseURL, BaseURLs: o.ProviderURLs,
		HTTPClient: &http.Client{}, Resolve: o.Resolve, Alerts: alerts,
	})
	brands := discovery.New(pool, settings, discovery.Options{Log: log})
	logoLookups := logos.New(pool, settings, logos.Options{Log: log, Client: sources.PublicClient(o.SourcesAllowPrivate)})
	runs := checks.New(pool, settings, checks.Options{Log: log, Alerts: alerts, Finder: brands})
	traffic := search.New(pool, search.Options{
		Secrets: o.Secrets, BaseURLs: map[search.Provider]string{search.Google: o.GSCBaseURL, search.Bing: o.BingBaseURL}, GoogleClient: o.GSCClient,
		PublicURL: o.PublicURL, Terms: settings, Alerts: alerts,
	})
	site := changes.New(pool, traffic, changes.Options{Secrets: o.Secrets, Alerts: alerts, GitHubBaseURL: o.GitHubBaseURL})
	mentions := social.New(pool, settings, social.Options{Alerts: alerts, BaseURLs: map[social.Network]string{social.Bluesky: o.BlueskyBaseURL}})
	svc := app.Services{
		Instance: instance.New(o.Version, pool, settings, mentions, brands), Config: settings, Checks: runs, Results: res, Notify: alerts,
		Search: traffic, Changes: site, Social: mentions, Discovery: brands, Logos: logoLookups,
	}
	cited := sources.New(pool, settings, sources.Options{AllowPrivate: o.SourcesAllowPrivate, UserAgent: "Byname/" + o.Version, Log: log})
	run := func(ctx context.Context) {
		var jobs sync.WaitGroup
		jobs.Go(func() {
			scheduler.New(runs, settings, scheduler.Options{Log: log, Sources: cited, Search: traffic, Changes: site, Social: mentions,
				OtherBrands: []scheduler.Ticker{brands, logoLookups}}).Run(ctx)
		})
		jobs.Go(func() { alerts.Run(ctx) })
		jobs.Wait()
	}
	return svc, run
}

func prepare(ctx context.Context, w db.Entity, svc app.Services, log *slog.Logger) error {
	if err := svc.Config.MarkBrandQuestions(ctx); err != nil {
		return fmt.Errorf("entity %s: mark the questions that name its brand: %w", w.Slug, err)
	}
	search, err := svc.Search.SealSecrets(ctx)
	if err != nil {
		return fmt.Errorf("entity %s: encrypt the search engine credentials with ENCRYPTION_KEY: %w", w.Slug, err)
	}
	repo, err := svc.Changes.SealSecrets(ctx)
	if err != nil {
		return fmt.Errorf("entity %s: encrypt the GitHub token with ENCRYPTION_KEY: %w", w.Slug, err)
	}
	channels, err := svc.Notify.SealChannels(ctx)
	if err != nil {
		return fmt.Errorf("entity %s: encrypt notification channel secrets with ENCRYPTION_KEY: %w", w.Slug, err)
	}
	if search+repo+channels > 0 {
		log.Info("encrypted stored secrets with the current ENCRYPTION_KEY", "entity", w.Slug,
			"credentials", search+repo, "notification_channels", channels)
	}
	return nil
}

const refreshEvery = time.Minute
