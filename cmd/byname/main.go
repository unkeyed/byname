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
	"github.com/unkeyed/byname/internal/checks"
	instconfig "github.com/unkeyed/byname/internal/config"
	"github.com/unkeyed/byname/internal/db"
	"github.com/unkeyed/byname/internal/engines"
	"github.com/unkeyed/byname/internal/httpapi"
	"github.com/unkeyed/byname/internal/instance"
	"github.com/unkeyed/byname/internal/mcpserver"
	"github.com/unkeyed/byname/internal/results"
	"github.com/unkeyed/byname/internal/scheduler"
	"github.com/unkeyed/byname/internal/secretbox"
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
	if raw := os.Getenv("TREG_BASE_URL"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return c, fmt.Errorf("TREG_BASE_URL must be an http(s) URL such as http://localhost:9999, got %q", raw)
		}
		c.TregBaseURL = raw
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

	pool, err := pgxpool.NewWithConfig(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrate(ctx, pool, cfg.MigrateDB); err != nil {
		return err
	}

	svc := instance.New(version)
	users, err := auth.NewUsers(pool, cfg.Auth)
	if err != nil {
		return err
	}
	settings := instconfig.New(pool, instconfig.Options{
		Secrets: cfg.Secrets, TregBaseURL: cfg.TregBaseURL,
		HTTPClient: &http.Client{}, Resolve: engines.HeadResolver(&http.Client{Timeout: 10 * time.Second}),
	})
	runs := checks.New(pool, settings, checks.Options{Log: log})
	if err := sealSecrets(ctx, log, users, settings); err != nil {
		return err
	}
	if err := announceSetup(ctx, log, users, cfg.SetupToken); err != nil {
		return err
	}
	authn := &auth.Authenticator{Keys: auth.NewKeyStore(pool, cfg.Unkey), Users: users, OAuth: auth.NewOAuthServer(pool, cfg.Auth.PublicURL)}

	services := app.Services{Instance: svc, Config: settings, Checks: runs, Results: results.New(pool)}
	mux := httpapi.NewMux(services, authn, log)
	mux.Handle("/mcp", mcpserver.Handler(services, authn.OAuth, version))
	mux.Handle("/", web.Handler())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: httpapi.SecurityHeaders(cfg.Auth.PublicURL, mux), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	if cfg.TregBaseURL != "" {
		log.Warn("TREG_BASE_URL is set: Treg calls go to it, not to Treg. Use it only in tests and local development", "treg_base_url", cfg.TregBaseURL)
	}
	var jobs sync.WaitGroup
	jobs.Go(func() { scheduler.New(runs, settings, scheduler.Options{Log: log}).Run(ctx) })
	log.Info("Byname listening", "port", cfg.Port, "version", version, "keys", authn.Keys.Mode(),
		"public_url", cfg.Auth.PublicURL.String(), "password_login", cfg.Auth.PasswordLogin, "providers", len(cfg.Auth.Providers))

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
func sealSecrets(ctx context.Context, log *slog.Logger, users *auth.Users, settings *instconfig.Service) error {
	totp, err := users.SealTOTPSecrets(ctx)
	if err != nil {
		return fmt.Errorf("encrypt TOTP seeds with ENCRYPTION_KEY: %w", err)
	}
	creds, err := settings.SealCredentials(ctx)
	if err != nil {
		return fmt.Errorf("encrypt credentials with ENCRYPTION_KEY: %w", err)
	}
	if totp+creds > 0 {
		log.Info("encrypted stored secrets with the current ENCRYPTION_KEY", "totp_seeds", totp, "credentials", creds)
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
