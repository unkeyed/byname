// Package httpapi serves the REST API, which is also the UI's API: the React
// app calls the same /v1 routes with a session cookie that agents call with
// an API key. Handlers decode, call one service method, encode. Any logic
// beyond that belongs in the service.
package httpapi

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/unkeyed/byname/internal/app"
	"github.com/unkeyed/byname/internal/auth"
	"github.com/unkeyed/byname/internal/core"
)

// access is who may call a route: one of the classes below, or the one
// core.Scope the caller needs, which a session, an API key or (on /mcp) an
// OAuth token can carry. TestEveryRouteHasAnAccessClass fails on anything
// else.
type access string

const (
	public  access = "public"  // no caller check: health, this document, sign-in
	session access = "session" // a signed-in person of any role: their own account
	admin   access = "admin"   // a signed-in admin: keys, apps, the team, credentials
	// anyone is any authenticated caller, scopes or not. Only DELETE
	// /v1/keys/current uses it, so a key that lost every scope can still
	// revoke itself.
	anyone access = "anyone"
)

func needs(s core.Scope) access { return access(s) }

type handlerFunc func(s *server, w http.ResponseWriter, r *http.Request, a core.Actor) error

type route struct {
	pattern string
	access  access
	handle  handlerFunc
}

// routes is the only list of HTTP endpoints. TestRoutesMatchOpenAPI fails
// when openapi.yaml and this table disagree.
var routes = []route{
	{"GET /healthz", public, (*server).healthz},
	{"GET /v1/openapi.yaml", public, (*server).openapi},

	{"GET /v1/status", needs(core.ResultsRead), (*server).status},

	{"GET /v1/settings", needs(core.ResultsRead), (*server).getSettings},
	{"PUT /v1/settings", needs(core.ConfigWrite), (*server).putSettings},
	{"GET /v1/brands", needs(core.ResultsRead), (*server).listBrands},
	{"POST /v1/brands", needs(core.ConfigWrite), (*server).upsertBrand},
	{"PUT /v1/brands/{id}", needs(core.ConfigWrite), (*server).updateBrand},
	{"DELETE /v1/brands/{id}", needs(core.ConfigWrite), (*server).deleteBrand},
	{"GET /v1/questions", needs(core.ResultsRead), (*server).getQuestions},
	{"PUT /v1/questions", needs(core.ConfigWrite), (*server).putQuestions},
	{"GET /v1/question-sets", needs(core.ResultsRead), (*server).listQuestionSets},
	{"GET /v1/question-sets/{id}", needs(core.ResultsRead), (*server).getQuestionSet},
	{"GET /v1/surfaces", needs(core.ResultsRead), (*server).listSurfaces},
	{"PUT /v1/surfaces/{key}", needs(core.ConfigWrite), (*server).putSurface},
	{"GET /v1/surfaces/{key}/models", needs(core.ResultsRead), (*server).listSurfaceModels},

	{"POST /v1/runs", needs(core.RunsWrite), (*server).runNow},
	{"POST /v1/checks/pause", needs(core.RunsWrite), (*server).pauseChecks},
	{"POST /v1/checks/resume", needs(core.RunsWrite), (*server).resumeChecks},

	{"GET /v1/runs", needs(core.ResultsRead), (*server).listRuns},
	{"GET /v1/runs/{id}", needs(core.ResultsRead), (*server).getRun},
	{"GET /v1/runs/{id}/answers", needs(core.ResultsRead), (*server).listRunAnswers},
	{"GET /v1/runs/{id}/questions", needs(core.ResultsRead), (*server).runQuestions},
	{"GET /v1/answers/{id}", needs(core.ResultsRead), (*server).getAnswer},
	{"GET /v1/compare", needs(core.ResultsRead), (*server).compareRuns},
	{"GET /v1/questions/{id}/history", needs(core.ResultsRead), (*server).questionHistory},
	{"GET /v1/metrics/rates", needs(core.ResultsRead), (*server).mentionRates},
	{"GET /v1/metrics/share", needs(core.ResultsRead), (*server).shareOfMentions},
	{"GET /v1/metrics/summary", needs(core.ResultsRead), (*server).summary},
	{"GET /v1/metrics/competitors", needs(core.ResultsRead), (*server).competitors},
	{"GET /v1/gaps", needs(core.ResultsRead), (*server).listGaps},
	{"GET /v1/jobs", needs(core.ResultsRead), (*server).listJobs},
	// Prompts, cited sources and the engine drawer.
	{"GET /v1/prompts", needs(core.ResultsRead), (*server).prompts},
	{"GET /v1/sources", needs(core.ResultsRead), (*server).citedSources},
	{"GET /v1/metrics/engine", needs(core.ResultsRead), (*server).engineView},
	// Notifications: in-app alerts.
	{"GET /v1/alerts", needs(core.ResultsRead), (*server).listAlerts},
	{"POST /v1/alerts/read", needs(core.ResultsRead), (*server).markAlertsRead},

	{"GET /v1/keys", admin, (*server).listKeys},
	{"POST /v1/keys", admin, (*server).createKey},
	{"DELETE /v1/keys/{id}", admin, (*server).revokeKey},
	{"DELETE /v1/keys/current", anyone, (*server).revokeCurrentKey},
	{"GET /v1/apps", admin, (*server).listApps},
	{"DELETE /v1/apps/{id}", admin, (*server).revokeApp},
	{"GET /v1/users", admin, (*server).listUsers},
	{"PATCH /v1/users/{id}", admin, (*server).setUserRole},
	{"DELETE /v1/users/{id}", admin, (*server).removeUser},
	{"POST /v1/invites", admin, (*server).createInvite},
	{"GET /v1/credentials", admin, (*server).listCredentials},
	{"PUT /v1/credentials/treg", admin, (*server).setTregToken},
	{"POST /v1/credentials/treg/test", admin, (*server).testTregToken},
	// Notifications: channels and the delivery log.
	{"GET /v1/notification-channels", admin, (*server).listChannels},
	{"POST /v1/notification-channels", admin, (*server).createChannel},
	{"PUT /v1/notification-channels/{id}", admin, (*server).updateChannel},
	{"DELETE /v1/notification-channels/{id}", admin, (*server).deleteChannel},
	{"POST /v1/notification-channels/{id}/test", admin, (*server).testChannel},
	{"GET /v1/notification-deliveries", admin, (*server).listDeliveries},

	{"GET /v1/oauth/requests/{id}", session, (*server).consentRequest},
	{"POST /v1/oauth/requests/{id}", session, (*server).decideConsent},
	{"GET /v1/me", session, (*server).me},
	{"GET /v1/account", session, (*server).account},
	{"POST /v1/account/passkeys/begin", session, (*server).addPasskeyBegin},
	{"POST /v1/account/passkeys", session, (*server).addPasskey},
	{"DELETE /v1/account/passkeys/{id}", session, (*server).removePasskey},
	{"DELETE /v1/account/identities/{provider}", session, (*server).unlinkIdentity},
	{"POST /v1/account/totp/begin", session, (*server).totpBegin},
	{"POST /v1/account/totp", session, (*server).totpFinish},
	{"DELETE /v1/account/totp", session, (*server).totpDisable},
	{"POST /v1/account/recovery-codes", session, (*server).regenerateRecoveryCodes},
	{"PUT /v1/account/password", session, (*server).setPassword},
	{"DELETE /v1/account/password", session, (*server).removePassword},
	{"DELETE /v1/account/sessions/{id}", session, (*server).revokeSession},
	{"POST /v1/account/reauth/passkey/begin", session, (*server).reauthPasskeyBegin},
	{"POST /v1/account/reauth/passkey", session, (*server).reauthPasskey},
	{"POST /v1/account/reauth/code", session, (*server).reauthCode},

	{"GET /auth/options", public, (*server).authOptions},
	{"POST /auth/passkey/begin", public, (*server).passkeyBegin},
	{"POST /auth/passkey/finish", public, (*server).passkeyFinish},
	{"POST /auth/password", public, (*server).passwordLogin},
	{"GET /auth/second-factor", public, (*server).secondFactor},
	{"POST /auth/second-factor/code", public, (*server).secondFactorCode},
	{"POST /auth/second-factor/passkey/begin", public, (*server).secondFactorPasskeyBegin},
	{"POST /auth/second-factor/passkey/finish", public, (*server).secondFactorPasskeyFinish},
	{"GET /auth/oauth/{provider}/start", public, (*server).oauthStart},
	{"GET /auth/oauth/{provider}/callback", public, (*server).oauthCallback},
	{"GET /auth/setup/{token}", public, (*server).checkSetup},
	{"GET /auth/invites/{token}", public, (*server).inviteInfo},
	{"POST /auth/signup/passkey/begin", public, (*server).signupPasskeyBegin},
	{"POST /auth/signup/passkey/finish", public, (*server).signupPasskeyFinish},
	{"POST /auth/logout", public, (*server).logout},
}

//go:embed openapi.yaml
var openapiYAML []byte

type server struct {
	svc   app.Services
	authn *auth.Authenticator
	keys  auth.KeyStore
	users *auth.Users
	oauth *auth.OAuthServer
	log   *slog.Logger
}

// NewMux mounts every route and the OAuth endpoints (mountOAuth) on a new
// mux; main adds /mcp and the UI's "/" to the same mux. Unmatched /v1/ paths get a JSON 405 with Allow when
// another method serves the path, else a JSON 404, instead of falling
// through to the UI. Browser-reachable routes are wrapped in
// http.CrossOriginProtection, which rejects cross-site unsafe requests and
// lets non-browser clients (no Sec-Fetch-Site / Origin) through.
func NewMux(svc app.Services, authn *auth.Authenticator, log *slog.Logger) *http.ServeMux {
	s := &server{svc: svc, authn: authn, keys: authn.Keys, users: authn.Users, oauth: authn.OAuth, log: log}
	csrf := http.NewCrossOriginProtection()
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.Handle(rt.pattern, csrf.Handler(s.wrap(rt)))
	}
	s.mountOAuth(mux)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		// Anonymous callers get the same 401 as on a real route, so a path's
		// existence is not an answer to someone without credentials.
		if _, err := s.authn.Caller(r); err != nil {
			s.writeError(w, r, callerErr(err))
			return
		}
		var allow []string
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			probe := *r
			probe.Method = m
			if _, pattern := mux.Handler(&probe); pattern != "/v1/" {
				allow = append(allow, m)
			}
		}
		if len(allow) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such route; the API is described at GET /v1/openapi.yaml"})
			return
		}
		w.Header().Set("Allow", strings.Join(allow, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": fmt.Sprintf("%s is not allowed on %s; use %s", r.Method, r.URL.Path, strings.Join(allow, " or ")),
		})
	})
	return mux
}

var (
	errSessionOnly = fmt.Errorf("%w: API keys cannot use this route; sign in to the UI", core.ErrForbidden)
	errAdminOnly   = fmt.Errorf("%w: only an admin can do this; ask an admin under Settings > Team", core.ErrForbidden)
	errUnavailable = errors.New("unavailable")
)

func (s *server) wrap(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var actor core.Actor
		if rt.access != public {
			a, err := s.authn.Caller(r)
			if err != nil {
				s.writeError(w, r, callerErr(err))
				return
			}
			if err := rt.access.allows(a); err != nil {
				s.writeError(w, r, err)
				return
			}
			actor = a
		}
		if err := rt.handle(s, w, r, actor); err != nil {
			s.writeError(w, r, err)
		}
	})
}

// allows refuses an authenticated caller the route is not for.
func (acc access) allows(a core.Actor) error {
	switch acc {
	case anyone:
		return nil
	case session, admin:
		if a.Via != core.ViaSession {
			return errSessionOnly
		}
		if acc == admin && a.Role != core.Admin {
			return errAdminOnly
		}
		return nil
	}
	scope := core.Scope(acc)
	switch {
	case a.Has(scope):
		return nil
	case a.Via == core.ViaAPIKey:
		return fmt.Errorf("%w: this API key does not have the %s scope; create a key with it under Settings > API keys", core.ErrForbidden, scope)
	}
	return fmt.Errorf("%w: the %s role does not include %s; ask an admin to change your role", core.ErrForbidden, a.Role, scope)
}

// callerErr marks a failure to authenticate that is not the caller's fault:
// Caller returns ErrUnauthorized or ErrInvalidKey for bad credentials, and
// anything else means the key store or database could not decide.
func callerErr(err error) error {
	if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrInvalidKey) {
		return err
	}
	return fmt.Errorf("%w: %w", errUnavailable, err)
}

// keyStoreErr does the same for key management. A KeyStore reports the
// caller's mistakes as core.ErrInvalid, core.ErrNotFound or
// core.ErrForbidden; any other error means Postgres or Unkey could not
// answer.
func keyStoreErr(err error) error {
	if err == nil || errors.Is(err, core.ErrInvalid) || errors.Is(err, core.ErrNotFound) || errors.Is(err, core.ErrForbidden) {
		return err
	}
	return fmt.Errorf("%w: %w", errUnavailable, err)
}

// writeError is the single error-to-status mapping for REST.
// Body: {"error": "<message>"}, plus "code": "reauth_required" when the
// person must sign in again, and "request_id" on a 5xx, whose cause is
// logged under the same id.
func (s *server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := statusOf(err)
	var throttled *auth.ThrottledError
	if errors.As(err, &throttled) {
		w.Header().Set("Retry-After", strconv.Itoa(int(throttled.RetryAfter.Seconds())))
	}
	switch {
	case errors.Is(err, auth.ErrReauth):
		writeJSON(w, code, map[string]string{"error": err.Error(), "code": "reauth_required"})
		return
	case code < 500, code == http.StatusBadGateway:
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	id := requestID()
	s.log.Error("request failed", "request_id", id, "status", code, "method", r.Method, "path", r.URL.Path, "err", err)
	msg := "internal error"
	if code == http.StatusServiceUnavailable {
		msg = "the database or key store is unavailable; retry shortly"
	}
	writeJSON(w, code, map[string]string{"error": msg, "request_id": id})
}

func statusOf(err error) int {
	var throttled *auth.ThrottledError
	var netErr net.Error
	switch {
	case errors.Is(err, core.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, auth.ErrUnauthorized), errors.Is(err, auth.ErrInvalidKey):
		return http.StatusUnauthorized
	case errors.Is(err, core.ErrForbidden), errors.Is(err, auth.ErrReauth):
		return http.StatusForbidden
	case errors.Is(err, core.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, core.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, core.ErrUpstream):
		return http.StatusBadGateway
	case errors.As(err, &throttled):
		return http.StatusTooManyRequests
	case errors.Is(err, errUnavailable), errors.As(err, &netErr):
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

func requestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// respond writes v with code, or returns err for writeError.
func respond(w http.ResponseWriter, code int, v any, err error) error {
	if err != nil {
		return err
	}
	writeJSON(w, code, v)
	return nil
}

func noContent(w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// readJSON decodes a body of at most 1 MiB, rejecting unknown fields, and
// wraps failures in core.ErrInvalid.
func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: request body is empty, want a JSON object", core.ErrInvalid)
		}
		return fmt.Errorf("%w: bad JSON body: %v", core.ErrInvalid, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: request body holds more than one JSON value", core.ErrInvalid)
	}
	return nil
}
