package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/unkeyed/byname/internal/core"
	"github.com/unkeyed/byname/internal/db"
)

// What only Google has: sign-in through the instance's OAuth client, where
// every other provider takes a pasted key, and URL Inspection.

// Return is the page the browser goes back to after Google's consent page:
// one of a fixed set, never a URL, so the callback cannot be made into an
// open redirect.
type Return string

const (
	ReturnSettings   Return = "settings"   // Settings > Search engines
	ReturnOnboarding Return = "onboarding" // onboarding's search engines step
)

// ParseReturn reads a Return at the boundary; "" is ReturnSettings.
func ParseReturn(s string) (Return, error) {
	switch r := Return(s); r {
	case "":
		return ReturnSettings, nil
	case ReturnSettings, ReturnOnboarding:
		return r, nil
	}
	return "", fmt.Errorf("%w: return_to must be settings or onboarding, got %q", core.ErrInvalid, s)
}

type oauthState struct {
	UserID   int64  `json:"u"`
	Verifier string `json:"v"`
	Expires  int64  `json:"e"`
	Return   Return `json:"r,omitempty"`
	Entity   int64  `json:"w,omitempty"`
}

// stateSeal is Search Console's own label from before Bing joined, so a
// sign-in started before an upgrade still finishes after it.
const stateSeal = "gsc:oauth-state"

// StartGoogleOAuth returns Google's consent page URL for the admin a,
// asking for read-only Search Console access with offline access, so
// Google returns a refresh token. The browser comes back to
// /auth/gsc/callback, which sends it on to back.
func (s *Service) StartGoogleOAuth(ctx context.Context, a core.Actor, back Return) (string, error) {
	if !s.google.client.set() {
		return "", fmt.Errorf("%w: Sign in with Google needs this instance's own Google OAuth client; set GSC_GOOGLE_CLIENT_ID and GSC_GOOGLE_CLIENT_SECRET, or connect a service account instead", core.ErrInvalid)
	}
	w, err := db.CurrentEntity(ctx, s.db)
	if err != nil {
		return "", err
	}
	st := oauthState{UserID: a.UserID, Verifier: oauth2.GenerateVerifier(), Expires: s.now().Add(15 * time.Minute).Unix(), Return: back, Entity: w.ID}
	raw, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	state := s.secrets.Seal(string(raw), stateSeal)
	return s.google.oauthConfig().AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(st.Verifier)), nil
}

// OAuthOrigin is where a Google sign-in started: the entity it
// connects, 0 for one started before there were entities, which is the
// first's, and the page the browser goes back to.
type OAuthOrigin struct {
	Entity int64
	Return Return
}

// GoogleOAuthOrigin reads where the sign-in that state belongs to started:
// what is sealed in it, or ReturnSettings in the first entity when state
// is not one this instance sealed.
func (s *Service) GoogleOAuthOrigin(state string) OAuthOrigin {
	var st oauthState
	raw, err := s.secrets.Open(state, stateSeal)
	if err != nil || json.Unmarshal([]byte(raw), &st) != nil {
		return OAuthOrigin{Return: ReturnSettings}
	}
	back, err := ParseReturn(string(st.Return))
	if err != nil {
		back = ReturnSettings
	}
	return OAuthOrigin{Entity: st.Entity, Return: back}
}

// OAuthCallback is what Google sends the browser back with.
type OAuthCallback struct {
	State string
	Code  string
	Error string // Google's error parameter, such as access_denied
}

// FinishGoogleOAuth exchanges the code for a refresh token and stores it
// sealed, keeping the site already picked, if any. Only the admin who
// started the connection can finish it.
func (s *Service) FinishGoogleOAuth(ctx context.Context, a core.Actor, cb OAuthCallback) error {
	if cb.Error != "" {
		return fmt.Errorf("%w: Google sign-in ended with %q; try again and allow Search Console access", core.ErrInvalid, cb.Error)
	}
	raw, err := s.secrets.Open(cb.State, stateSeal)
	var st oauthState
	if err != nil || json.Unmarshal([]byte(raw), &st) != nil || st.UserID != a.UserID {
		return fmt.Errorf("%w: this Google sign-in was not started here by you; start it again from Settings > Search engines", core.ErrInvalid)
	}
	if s.now().Unix() > st.Expires {
		return fmt.Errorf("%w: the Google sign-in took longer than 15 minutes; start it again", core.ErrInvalid)
	}
	if cb.Code == "" {
		return fmt.Errorf("%w: Google sent no authorization code; start the sign-in again", core.ErrInvalid)
	}
	tok, err := s.google.oauthConfig().Exchange(s.google.httpContext(ctx), cb.Code, oauth2.VerifierOption(st.Verifier))
	if re := (*oauth2.RetrieveError)(nil); errors.As(err, &re) {
		return tokenError(re)
	}
	if err != nil {
		return fmt.Errorf("%w: Google's token endpoint did not answer: %v", core.ErrUpstream, err)
	}
	if granted, _ := tok.Extra("scope").(string); granted != "" && !slices.Contains(strings.Fields(granted), GoogleScope) {
		return fmt.Errorf("%w: Google did not grant Search Console access; sign in again and leave the Search Console box ticked", core.ErrInvalid)
	}
	if tok.RefreshToken == "" {
		return fmt.Errorf("%w: Google sent no refresh token, so Byname could not sync after today; remove Byname's access at myaccount.google.com/permissions and sign in again", core.ErrUpstream)
	}
	return s.store(ctx, Google, MethodOAuth, "", tok.RefreshToken)
}

// InspectURL asks Google's URL Inspection (urlInspection.index.inspect)
// how the connected property's index sees pageURL: the coverage state,
// when Google last crawled it, and the canonical Google chose against the
// one the page declares. pageURL must be inside the property. Each call
// spends one of the property's 2,000 daily inspections, so recrawl watch
// calls it for changed pages only.
func (s *Service) InspectURL(ctx context.Context, pageURL string) (Inspection, error) {
	c, err := s.connection(ctx, Google)
	if err != nil {
		return Inspection{}, err
	}
	if c.site == "" {
		return Inspection{}, fmt.Errorf("%w: pick a Search Console property before inspecting URLs", core.ErrInvalid)
	}
	api, err := s.google.api(ctx, c.cred)
	if err != nil {
		return Inspection{}, err
	}
	return api.inspect(ctx, c.site, pageURL)
}
