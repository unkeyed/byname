// Command byname is the whole product in one process: REST + UI API, MCP
// and the embedded React app.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/changes"
	"github.com/unkeyed/byname/internal/checks"
	instconfig "github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/discovery"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/gsc"
	"github.com/unkeyed/byname/internal/httpapi"
	"github.com/unkeyed/byname/internal/instance"
	"github.com/unkeyed/byname/internal/logos"
	"github.com/unkeyed/byname/internal/mcpserver"
	"github.com/unkeyed/byname/internal/notify"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/scheduler"
	"github.com/unkeyed/byname/internal/secretbox"
	"github.com/unkeyed/byname/internal/social"
	"github.com/unkeyed/byname/internal/sources"
	"github.com/unkeyed/byname/web"
)

var version = "dev" // set by -ldflags at build

// config is every environment variable Byname reads, parsed once at boot.
// DATABASE_URL and ENCRYPTION_KEY are required.
type config struct {
	DB         *pgxpool.Config  // DATABASE_URL, required
	MigrateDB  *pgxpool.Config  // MIGRATION_DATABASE_URL, optional: migrations run as this role instead of DATABASE_URL's
	Port       string           // PORT, default 8080 (Unkey sets it)
	Unkey      auth.UnkeyConfig // UNKEY_ROOT_KEY + UNKEY_API_ID, both or neither
	Auth       auth.Config      // PUBLIC_URL, PASSWORD_LOGIN, ALLOWED_EMAIL_DOMAINS, CLIENT_IP_HEADER, providers
	SetupToken string           // SETUP_TOKEN, optional: the setup link's token instead of a random one
	Secrets    *secretbox.Box   // ENCRYPTION_KEY, required, and ENCRYPTION_KEY_PREVIOUS while rotating
	// TREG_BASE_URL, optional and for tests and local development only:
	// sends every Treg call to a fake Treg at this URL instead of
	// https://treg.to. Never set it in production.
	TregBaseURL string
	// SOURCES_ALLOW_PRIVATE_IPS, optional and for tests and local
	// development only: lets cited-source fetching and logo downloads
	// reach private and loopback addresses, such as a fake site on
	// localhost.
	SourcesAllowPrivate bool
	// RESEND_BASE_URL, optional and for tests and local development only:
	// sends email API channels to a fake Resend at this URL instead of
	// https://api.resend.com.
	ResendBaseURL string
	// OPENAI_BASE_URL and the rest of providerURLEnv do the same for
	// surfaces routed to a provider's own key.
	ProviderURLs map[engines.CredentialKind]string
	// GSC_GOOGLE_CLIENT_ID + GSC_GOOGLE_CLIENT_SECRET, both or neither: the
	// instance's own Google OAuth client for Sign in with Google on the
	// Search Console connection.
	GSCClient gsc.OAuthClient
	// GSC_GOOGLE_BASE_URL, optional and for tests and local development
	// only: sends every Google call the Search Console connection makes to
	// a fake Google at this URL. Never set it in production.
	GSCBaseURL string
	// GITHUB_API_BASE_URL, optional and for tests and local development
	// only: sends every GitHub call site changes makes to a fake GitHub at
	// this URL. Never set it in production.
	GitHubBaseURL string
	// BLUESKY_BASE_URL, optional and for tests and local development
	// only: sends social search on Bluesky to a fake at this URL. Never
	// set it in production.
	BlueskyBaseURL string
}

// providerURLEnv names each provider's base URL override.
var providerURLEnv = map[engines.CredentialKind]string{
	engines.CredentialOpenAI:     "OPENAI_BASE_URL",
	engines.CredentialAnthropic:  "ANTHROPIC_BASE_URL",
	engines.CredentialPerplexity: "PERPLEXITY_BASE_URL",
	engines.CredentialGoogle:     "GEMINI_BASE_URL",
	engines.CredentialDataForSEO: "DATAFORSEO_BASE_URL",
	engines.CredentialCloro:      "CLORO_BASE_URL",
	engines.CredentialSerpApi:    "SERPAPI_BASE_URL",
}

func baseURLEnv(name string) (string, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%s must be an http(s) URL such as http://localhost:9999, got %q", name, raw)
	}
	return raw, nil
}

// loadSecrets reads ENCRYPTION_KEY and the optional ENCRYPTION_KEY_PREVIOUS.
func loadSecrets() (*secretbox.Box, error) {
	raw := os.Getenv("ENCRYPTION_KEY")
	if raw == "" {
		return nil, errors.New("ENCRYPTION_KEY is required: it encrypts provider keys and TOTP seeds in the database. Generate one with openssl rand -base64 32 and keep it with your other secrets")
	}
	cur, err := secretbox.ParseKey(raw)
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY: %w", err)
	}
	var prev []byte
	if raw := os.Getenv("ENCRYPTION_KEY_PREVIOUS"); raw != "" {
		if prev, err = secretbox.ParseKey(raw); err != nil {
			return nil, fmt.Errorf("ENCRYPTION_KEY_PREVIOUS: %w", err)
		}
	}
	return secretbox.New(cur, prev)
}

// loadConfig reads and validates the environment. Every config error is
// reported here, before anything connects, with the variable's name.
// defaultPoolSize is how many connections Byname opens in all when
// DATABASE_URL does not set pool_max_conns. openLanes splits them.
const defaultPoolSize = 20

// openLanes opens the three pools Lanes routes between. Four connections
// hold locks, one for each job the scheduler can run at once; the rest of
// the budget is split between requests, which get half rounded up, and
// background work, with at least 2 each.
func openLanes(ctx context.Context, cfg *pgxpool.Config) (db.Lanes, error) {
	const lockConns = 4
	web, jobs, locks := cfg.Copy(), cfg.Copy(), cfg.Copy()
	locks.MaxConns = lockConns
	web.MaxConns = max(2, (cfg.MaxConns-lockConns+1)/2)
	jobs.MaxConns = max(2, cfg.MaxConns-lockConns-web.MaxConns)
	var l db.Lanes
	for _, p := range []struct {
		to  **pgxpool.Pool
		cfg *pgxpool.Config
	}{{&l.Web, web}, {&l.Jobs, jobs}, {&l.Locks, locks}} {
		pool, err := pgxpool.NewWithConfig(ctx, p.cfg)
		if err != nil {
			l.Close()
			return db.Lanes{}, err
		}
		*p.to = pool
	}
	return l, nil
}

func loadConfig() (config, error) {
	c := config{
		Port:       os.Getenv("PORT"),
		Unkey:      auth.UnkeyConfig{RootKey: os.Getenv("UNKEY_ROOT_KEY"), APIID: os.Getenv("UNKEY_API_ID")},
		SetupToken: os.Getenv("SETUP_TOKEN"),
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return c, errors.New("DATABASE_URL is required, e.g. postgres://user:pass@host:5432/byname")
	}
	db, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		// pgx's message can echo the URL, password included, so it is not logged.
		return c, errors.New("DATABASE_URL is not a Postgres connection string; want postgres://user:pass@host:5432/byname")
	}
	// pgx sizes the pool from the CPU count, 4 on a small instance, too few
	// to split between requests and background work, so the budget is
	// larger unless the URL sets it.
	if !strings.Contains(dbURL, "pool_max_conns") {
		db.MaxConns = defaultPoolSize
	}
	c.DB = db
	if migrateURL := os.Getenv("MIGRATION_DATABASE_URL"); migrateURL != "" {
		if c.MigrateDB, err = pgxpool.ParseConfig(migrateURL); err != nil {
			return c, errors.New("MIGRATION_DATABASE_URL is not a Postgres connection string; want postgres://user:pass@host:5432/byname")
		}
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	if n, err := strconv.Atoi(c.Port); err != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("PORT must be a port number, got %q", c.Port)
	}
	if (c.Unkey.RootKey == "") != (c.Unkey.APIID == "") {
		return c, errors.New("set both UNKEY_ROOT_KEY and UNKEY_API_ID to use Unkey keys, or neither for built-in keys")
	}
	if c.SetupToken != "" && len(c.SetupToken) < auth.MinSetupTokenLen {
		return c, fmt.Errorf("SETUP_TOKEN must be at least %d characters; generate one with openssl rand -hex 32", auth.MinSetupTokenLen)
	}
	if c.Secrets, err = loadSecrets(); err != nil {
		return c, err
	}
	if c.TregBaseURL, err = baseURLEnv("TREG_BASE_URL"); err != nil {
		return c, err
	}
	c.ProviderURLs = map[engines.CredentialKind]string{}
	for kind, name := range providerURLEnv {
		if c.ProviderURLs[kind], err = baseURLEnv(name); err != nil {
			return c, err
		}
	}
	if v := os.Getenv("SOURCES_ALLOW_PRIVATE_IPS"); v != "" {
		if c.SourcesAllowPrivate, err = strconv.ParseBool(v); err != nil {
			return c, fmt.Errorf("SOURCES_ALLOW_PRIVATE_IPS must be true or false, got %q", v)
		}
	}
	if raw := os.Getenv("RESEND_BASE_URL"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return c, fmt.Errorf("RESEND_BASE_URL must be an http(s) URL such as http://localhost:9998, got %q", raw)
		}
		c.ResendBaseURL = raw
	}
	if c.GSCBaseURL, err = baseURLEnv("GSC_GOOGLE_BASE_URL"); err != nil {
		return c, err
	}
	if c.GitHubBaseURL, err = baseURLEnv("GITHUB_API_BASE_URL"); err != nil {
		return c, err
	}
	if c.BlueskyBaseURL, err = baseURLEnv("BLUESKY_BASE_URL"); err != nil {
		return c, err
	}
	c.GSCClient = gsc.OAuthClient{ID: os.Getenv("GSC_GOOGLE_CLIENT_ID"), Secret: os.Getenv("GSC_GOOGLE_CLIENT_SECRET")}
	if (c.GSCClient.ID == "") != (c.GSCClient.Secret == "") {
		return c, errors.New("set both GSC_GOOGLE_CLIENT_ID and GSC_GOOGLE_CLIENT_SECRET to connect Search Console with Google sign-in, or neither to connect it with a service account only")
	}
	c.Auth, err = loadAuthConfig(c.Port)
	c.Auth.Secrets = c.Secrets
	return c, err
}

func loadAuthConfig(port string) (auth.Config, error) {
	var c auth.Config
	raw := cmp.Or(os.Getenv("PUBLIC_URL"), "http://localhost:"+port)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" {
		return c, fmt.Errorf("PUBLIC_URL must be an origin such as https://byname.example.com, got %q", raw)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return c, fmt.Errorf("PUBLIC_URL must use https unless its host is localhost: passkeys need a secure origin and session cookies must not travel in the clear, got %q", raw)
	}
	u.Path = ""
	c.PublicURL = u
	if v := os.Getenv("PASSWORD_LOGIN"); v != "" {
		if c.PasswordLogin, err = strconv.ParseBool(v); err != nil {
			return c, fmt.Errorf("PASSWORD_LOGIN must be true or false, got %q", v)
		}
	}
	for _, d := range strings.Split(os.Getenv("ALLOWED_EMAIL_DOMAINS"), ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			if strings.Contains(d, "@") {
				return c, fmt.Errorf("ALLOWED_EMAIL_DOMAINS lists domains such as example.com, got %q", d)
			}
			c.AllowedEmailDomains = append(c.AllowedEmailDomains, d)
		}
	}
	if v := os.Getenv("ALLOWED_EMAIL_DOMAINS_ROLE"); v != "" {
		r, err := core.ParseRole(strings.ToLower(strings.TrimSpace(v)))
		if err != nil || r == core.Admin {
			return c, fmt.Errorf("ALLOWED_EMAIL_DOMAINS_ROLE must be viewer or editor, got %q; invite admins by name, since an admin manages credentials and the team", v)
		}
		c.DomainRole = r
	}
	c.ClientIPHeader = os.Getenv("CLIENT_IP_HEADER")
	for _, p := range []struct {
		cfg  auth.ProviderConfig
		vars string
	}{
		{auth.ProviderConfig{ID: "google", Name: "Google", Issuer: "https://accounts.google.com",
			ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET")},
			"GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET"},
		{auth.ProviderConfig{ID: "github", Name: "GitHub",
			ClientID: os.Getenv("GITHUB_CLIENT_ID"), ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET")},
			"GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET"},
		{auth.ProviderConfig{ID: "oidc", Name: cmp.Or(os.Getenv("OIDC_NAME"), "SSO"), Issuer: os.Getenv("OIDC_ISSUER"),
			ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET")},
			"OIDC_ISSUER, OIDC_CLIENT_ID and OIDC_CLIENT_SECRET"},
	} {
		required := []string{p.cfg.ClientID, p.cfg.ClientSecret}
		if p.cfg.ID == "oidc" {
			required = append(required, p.cfg.Issuer)
		}
		missing := 0
		for _, v := range required {
			if v == "" {
				missing++
			}
		}
		if missing == len(required) {
			continue
		}
		if missing > 0 {
			return c, fmt.Errorf("set all of %s, or none", p.vars)
		}
		c.Providers = append(c.Providers, p.cfg)
	}
	return c, nil
}

// migrate applies the migrations as MIGRATION_DATABASE_URL's role when it
// is set, so DATABASE_URL can be a role without DDL rights.
func migrate(ctx context.Context, pool *pgxpool.Pool, migrateDB *pgxpool.Config) error {
	if migrateDB == nil {
		if err := db.Migrate(ctx, pool); err != nil {
			if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "42501" {
				return fmt.Errorf("migrate the database at DATABASE_URL: %w; this role cannot create tables, so apply the migrations as one that can with `MIGRATION_DATABASE_URL=... byname migrate`, or set MIGRATION_DATABASE_URL here", err)
			}
			return fmt.Errorf("migrate the database at DATABASE_URL: %w", err)
		}
		return nil
	}
	migratePool, err := pgxpool.NewWithConfig(ctx, migrateDB)
	if err != nil {
		return err
	}
	defer migratePool.Close()
	if err := db.Migrate(ctx, migratePool); err != nil {
		return fmt.Errorf("migrate the database at MIGRATION_DATABASE_URL: %w", err)
	}
	return nil
}

// runMigrate is `byname migrate`: apply the migrations and exit, as
// MIGRATION_DATABASE_URL's role, or DATABASE_URL's when it is unset. It
// lets a deployment whose DATABASE_URL cannot create tables keep the admin
// URL out of its environment.
func runMigrate(log *slog.Logger) error {
	name := "MIGRATION_DATABASE_URL"
	url := os.Getenv(name)
	if url == "" {
		name = "DATABASE_URL"
		url = os.Getenv(name)
	}
	if url == "" {
		return errors.New("byname migrate needs MIGRATION_DATABASE_URL or DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("%s is not a Postgres connection string; want postgres://user:pass@host:5432/byname", name)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate the database at %s: %w", name, err)
	}
	log.Info("migrations applied", "via", name)
	return nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(log); err != nil {
			log.Error("migrate failed", "err", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "recover" {
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: byname recover <email>")
			os.Exit(2)
		}
		if err := runRecover(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "recover failed:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(log); err != nil {
		log.Error("Byname stopped", "err", err)
		os.Exit(1)
	}
}

// shutdownGrace is how long in-flight requests get after SIGTERM, inside
// the 30s most platforms wait before SIGKILL.
const shutdownGrace = 20 * time.Second

// run serves until SIGTERM or Ctrl-C. Then it stops accepting connections,
// lets in-flight requests finish, and only then closes the database pool.
func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	pool, err := openLanes(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrate(ctx, pool.Web, cfg.MigrateDB); err != nil {
		return err
	}

	users, err := auth.NewUsers(pool, cfg.Auth)
	if err != nil {
		return err
	}
	res := results.New(pool)
	alerts := notify.New(pool, res, notify.Options{
		Secrets: cfg.Secrets, PublicURL: cfg.Auth.PublicURL, HTTPClient: &http.Client{}, ResendBaseURL: cfg.ResendBaseURL, Log: log,
	})
	settings := instconfig.New(pool, instconfig.Options{
		Secrets: cfg.Secrets, TregBaseURL: cfg.TregBaseURL, BaseURLs: cfg.ProviderURLs,
		HTTPClient: &http.Client{}, Resolve: engines.HeadResolver(&http.Client{Timeout: 10 * time.Second}),
		Alerts: alerts,
	})
	brands := discovery.New(pool, settings, discovery.Options{Log: log})
	logoLookups := logos.New(pool, settings, logos.Options{Log: log, Client: sources.PublicClient(cfg.SourcesAllowPrivate)})
	runs := checks.New(pool, settings, checks.Options{Log: log, Alerts: alerts, Finder: brands})
	search := gsc.New(pool, gsc.Options{
		Secrets: cfg.Secrets, Endpoints: gsc.GoogleEndpoints(cfg.GSCBaseURL), Client: cfg.GSCClient,
		PublicURL: cfg.Auth.PublicURL, Terms: settings, Alerts: alerts,
	})
	site := changes.New(pool, search, changes.Options{Secrets: cfg.Secrets, Alerts: alerts, GitHubBaseURL: cfg.GitHubBaseURL})
	mentions := social.New(pool, settings, social.Options{Alerts: alerts, BaseURLs: map[social.Network]string{social.Bluesky: cfg.BlueskyBaseURL}})
	if err := sealSecrets(ctx, log, users, settings, alerts, search, site); err != nil {
		return err
	}
	if err := announceSetup(ctx, log, users, cfg.SetupToken); err != nil {
		return err
	}
	if err := settings.MarkBrandQuestions(ctx); err != nil {
		return err
	}
	authn := &auth.Authenticator{Keys: auth.NewKeyStore(pool, cfg.Unkey), Users: users, OAuth: auth.NewOAuthServer(pool, cfg.Auth.PublicURL)}

	services := app.Services{Instance: instance.New(version, pool, settings, mentions, brands), Config: settings, Checks: runs, Results: res, Notify: alerts,
		GSC: search, Changes: site, Social: mentions, Discovery: brands, Logos: logoLookups}
	mux := httpapi.NewMux(services, authn, log)
	mux.Handle("/mcp", mcpserver.Handler(services, authn.OAuth, version))
	mux.Handle("/", web.Handler())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: httpapi.SecurityHeaders(cfg.Auth.PublicURL, mux), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	if cfg.TregBaseURL != "" {
		log.Warn("TREG_BASE_URL is set: Treg calls go to it, not to Treg. Use it only in tests and local development", "treg_base_url", cfg.TregBaseURL)
	}
	if cfg.ResendBaseURL != "" {
		log.Warn("RESEND_BASE_URL is set: email API channels send to it, not to Resend. Use it only in tests and local development", "resend_base_url", cfg.ResendBaseURL)
	}
	for kind, u := range cfg.ProviderURLs {
		if u != "" {
			log.Warn(providerURLEnv[kind]+" is set: calls go to it, not to the provider. Use it only in tests and local development", "url", u)
		}
	}
	if cfg.GitHubBaseURL != "" {
		log.Warn("GITHUB_API_BASE_URL is set: site changes call it, not GitHub. Use it only in tests and local development", "github_api_base_url", cfg.GitHubBaseURL)
	}
	if cfg.BlueskyBaseURL != "" {
		log.Warn("BLUESKY_BASE_URL is set: social search on Bluesky calls it, not Bluesky. Use it only in tests and local development", "bluesky_base_url", cfg.BlueskyBaseURL)
	}
	if cfg.GSCBaseURL != "" {
		log.Warn("GSC_GOOGLE_BASE_URL is set: Search Console calls go to it, not to Google. Use it only in tests and local development", "gsc_google_base_url", cfg.GSCBaseURL)
	}
	var jobs sync.WaitGroup
	if cfg.SourcesAllowPrivate {
		log.Warn("SOURCES_ALLOW_PRIVATE_IPS is set: cited-source fetching and logo downloads may reach private and loopback addresses. Use it only in tests and local development")
	}
	cited := sources.New(pool, settings, sources.Options{AllowPrivate: cfg.SourcesAllowPrivate, UserAgent: "Byname/" + version, Log: log})
	jobs.Go(func() {
		scheduler.New(runs, settings, scheduler.Options{Log: log, Sources: cited, Search: search, Changes: site, Social: mentions,
			OtherBrands: []scheduler.Ticker{brands, logoLookups}}).Run(db.Background(ctx))
	})
	jobs.Go(func() { alerts.Run(db.Background(ctx)) })
	log.Info("Byname listening", "port", cfg.Port, "version", version, "keys", authn.Keys.Mode(),
		"public_url", cfg.Auth.PublicURL.String(), "password_login", cfg.Auth.PasswordLogin, "providers", len(cfg.Auth.Providers),
		"db_pool_web", pool.Web.Config().MaxConns, "db_pool_jobs", pool.Jobs.Config().MaxConns, "db_pool_locks", pool.Locks.Config().MaxConns)

	select {
	case err := <-served:
		stop()
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "grace", shutdownGrace.String())
	drain, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(drain); err != nil {
		return fmt.Errorf("drain HTTP: %w", err)
	}
	log.Info("HTTP drained")
	// The scheduler stops queueing calls when ctx ends; a run in progress
	// stays running and the next start resumes it.
	stopped := make(chan struct{})
	go func() { jobs.Wait(); close(stopped) }()
	select {
	case <-stopped:
		log.Info("scheduler stopped")
	case <-drain.Done():
		return errors.New("the scheduler did not stop within the shutdown grace; its run resumes on the next start")
	}
	return nil
}

// sealSecrets reseals TOTP seeds and credentials sealed under
// ENCRYPTION_KEY_PREVIOUS with ENCRYPTION_KEY, so a rotation finishes with
// one restart.
func sealSecrets(ctx context.Context, log *slog.Logger, users *auth.Users, settings *instconfig.Service, alerts *notify.Service, search *gsc.Service, site *changes.Service) error {
	totp, err := users.SealTOTPSecrets(ctx)
	if err != nil {
		return fmt.Errorf("encrypt TOTP seeds with ENCRYPTION_KEY: %w", err)
	}
	creds, err := settings.SealCredentials(ctx)
	if err != nil {
		return fmt.Errorf("encrypt credentials with ENCRYPTION_KEY: %w", err)
	}
	gscCreds, err := search.SealSecrets(ctx)
	if err != nil {
		return fmt.Errorf("encrypt the Search Console credential with ENCRYPTION_KEY: %w", err)
	}
	creds += gscCreds
	repoCreds, err := site.SealSecrets(ctx)
	if err != nil {
		return fmt.Errorf("encrypt the GitHub token with ENCRYPTION_KEY: %w", err)
	}
	creds += repoCreds
	channels, err := alerts.SealChannels(ctx)
	if err != nil {
		return fmt.Errorf("encrypt notification channel secrets with ENCRYPTION_KEY: %w", err)
	}
	if totp+creds+channels > 0 {
		log.Info("encrypted stored secrets with the current ENCRYPTION_KEY", "totp_seeds", totp, "credentials", creds, "notification_channels", channels)
	}
	return nil
}

// runRecover is `byname recover <email>`: print a one-time link, valid for
// an hour, that lets that person register a new passkey after losing every
// way to sign in. It needs DATABASE_URL and PUBLIC_URL, the same values the
// server runs with.
func runRecover(email string) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("byname recover needs DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return errors.New("DATABASE_URL is not a Postgres connection string")
	}
	authCfg, err := loadAuthConfig(cmp.Or(os.Getenv("PORT"), "8080"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	link, err := auth.IssueRecovery(ctx, pool, authCfg.PublicURL, email)
	if err != nil {
		return err
	}
	fmt.Printf("\n  Send this link to %s. It works once, for an hour, and signs out their other sessions:\n\n    %s\n\n", email, link)
	if os.Getenv("PUBLIC_URL") == "" {
		fmt.Fprintln(os.Stderr, "  PUBLIC_URL is unset, so the link points at localhost. Set PUBLIC_URL to the address people open Byname at.")
	}
	return nil
}

// announceSetup logs the setup link while nobody can sign in, as a log
// field and as a plain banner on stderr that is easy to spot in
// docker compose up or make dev.
func announceSetup(ctx context.Context, log *slog.Logger, users *auth.Users, fixed string) error {
	token, err := users.PrepareSetup(ctx, fixed)
	if err != nil || token == "" {
		return err
	}
	link := users.SetupURL(token)
	log.Warn("nobody can sign in yet: open the setup link to create the first account", "setup_url", link)
	fmt.Fprintf(os.Stderr, "\n  Byname has no account that can sign in yet. Open this link to create the first one, an admin:\n\n    %s\n\n  It works once and stays valid until someone uses it.\n\n", link)
	return nil
}
