package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/unkeyed/byname/internal/core"
)

// ChannelKind is one way to send alerts. The channelKinds table below is
// the only list of them.
type ChannelKind string

const (
	SMTP         ChannelKind = "smtp"
	EmailAPI     ChannelKind = "email_api"
	SlackWebhook ChannelKind = "slack_webhook"
	Webhook      ChannelKind = "webhook"
)

// transport is one channel's settings and secrets, parsed and valid, and
// how it sends.
type transport interface {
	send(ctx context.Context, s *Service, e envelope) error
	// parts are the settings shown in the clear and the secrets sealed.
	parts() (settings, secrets any)
}

// channelKind parses a channel's stored or submitted JSON into its
// transport. secrets are applied in order, so a stored set followed by an
// edit's partial set keeps every secret the edit leaves out.
type channelKind struct {
	parse func(settings json.RawMessage, secrets ...json.RawMessage) (transport, error)
	// generated is set for a kind whose secret Byname makes at creation
	// and shows once, and that an edit cannot set.
	generated func(transport) string
}

var channelKinds = map[ChannelKind]channelKind{
	SMTP:         {parse: parseInto[smtpChannel]},
	EmailAPI:     {parse: parseInto[resendChannel]},
	SlackWebhook: {parse: parseInto[slackChannel]},
	Webhook: {parse: parseInto[webhookChannel], generated: func(t transport) string {
		w := t.(*webhookChannel)
		if w.Secrets.SigningSecret == "" {
			b := make([]byte, 24)
			_, _ = rand.Read(b)
			w.Secrets.SigningSecret = "whsec_" + hex.EncodeToString(b)
		}
		return w.Secrets.SigningSecret
	}},
}

// configurable is a transport whose settings and secrets decode into its
// own fields and then validate.
type configurable[T any] interface {
	*T
	transport
	fields() (settings, secrets any)
	validate() error
}

func parseInto[T any, P configurable[T]](settings json.RawMessage, secrets ...json.RawMessage) (transport, error) {
	t := P(new(T))
	set, sec := t.fields()
	if err := strict(settings, set); err != nil {
		return nil, fmt.Errorf("%w: settings: %v", core.ErrInvalid, err)
	}
	for _, s := range secrets {
		if err := strict(s, sec); err != nil {
			return nil, fmt.Errorf("%w: secrets: %v", core.ErrInvalid, err)
		}
	}
	if err := t.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrInvalid, err)
	}
	return t, nil
}

// strict decodes raw into v, refusing fields v does not have. Stored JSON
// is always written from the same structs, so it passes too.
func strict(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// envelope is one message on its way to one channel.
type envelope struct {
	// ID is unique per alert and channel, so a retried send can be
	// recognised: "alert-12/channel-3", or "test-..." for Send test.
	ID        string
	Kind      Kind
	Message   Message
	URL       string
	Settings  string
	Channel   string
	Payload   json.RawMessage
	CreatedAt time.Time
}

func (e envelope) text() string {
	var b strings.Builder
	b.WriteString(e.Message.Title + "\n\n")
	for _, l := range e.Message.Lines {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "\n%s\n\n-- \nSent by Byname to the %q channel. Choose what it gets at %s\n", e.URL, e.Channel, e.Settings)
	return b.String()
}

type smtpChannel struct {
	Settings struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		// TLS is starttls, tls (implicit, usually port 465) or none, which
		// is for a relay on the same host or network only.
		TLS      string   `json:"tls"`
		Username string   `json:"username"`
		From     string   `json:"from"`
		To       []string `json:"to"`
	}
	Secrets struct {
		Password string `json:"password"`
	}
}

func (c *smtpChannel) fields() (any, any) { return &c.Settings, &c.Secrets }
func (c *smtpChannel) parts() (any, any)  { return c.Settings, c.Secrets }

var smtpTLSModes = []string{"starttls", "tls", "none"}

func (c *smtpChannel) validate() error {
	s := &c.Settings
	s.Host = strings.TrimSpace(s.Host)
	if s.Host == "" || strings.ContainsAny(s.Host, " /:") {
		return errors.New("host must be the SMTP server's hostname, such as smtp.example.com")
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("port must be 1 to 65535, usually 587 for STARTTLS or 465 for TLS")
	}
	if !slices.Contains(smtpTLSModes, s.TLS) {
		return fmt.Errorf("tls must be one of %s", strings.Join(smtpTLSModes, ", "))
	}
	if (s.Username == "") != (c.Secrets.Password == "") {
		return errors.New("give both a username and a password, or neither for a relay that needs no sign-in")
	}
	var err error
	if s.From, err = address("from", s.From); err != nil {
		return err
	}
	s.To, err = recipients(s.To)
	return err
}

func (c *smtpChannel) send(ctx context.Context, s *Service, e envelope) error {
	set := c.Settings
	from, _ := mail.ParseAddress(set.From)
	addr := net.JoinHostPort(set.Host, strconv.Itoa(set.Port))
	tlsConfig := &tls.Config{ServerName: set.Host, RootCAs: s.rootCAs, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if set.TLS == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(sendTimeout))
	client, err := smtp.NewClient(conn, set.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if set.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS; pick TLS or check the port")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if set.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", set.Username, c.Secrets.Password, set.Host)); err != nil {
			return fmt.Errorf("sign in as %s: %w", set.Username, err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	for _, to := range set.To {
		a, _ := mail.ParseAddress(to)
		if err := client.Rcpt(a.Address); err != nil {
			return fmt.Errorf("recipient %s: %w", a.Address, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(emailMessage(from.String(), set.To, e, s.now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func emailMessage(from string, to []string, e envelope, now time.Time) []byte {
	var b bytes.Buffer
	for _, h := range [][2]string{
		{"From", from},
		{"To", strings.Join(to, ", ")},
		{"Subject", mime.QEncoding.Encode("utf-8", e.Message.Title)},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", fmt.Sprintf("<%s.%d@byname>", strings.ReplaceAll(e.ID, "/", "."), now.UnixNano())},
		{"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=utf-8"},
		{"Content-Transfer-Encoding", "quoted-printable"},
	} {
		b.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(e.text(), "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}

// resendChannel sends through Resend's HTTP API (POST /emails), the one
// email API Byname speaks.
type resendChannel struct {
	Settings struct {
		From string   `json:"from"`
		To   []string `json:"to"`
	}
	Secrets struct {
		APIKey string `json:"api_key"`
	}
}

func (c *resendChannel) fields() (any, any) { return &c.Settings, &c.Secrets }
func (c *resendChannel) parts() (any, any)  { return c.Settings, c.Secrets }

func (c *resendChannel) validate() error {
	c.Secrets.APIKey = strings.TrimSpace(c.Secrets.APIKey)
	if c.Secrets.APIKey == "" || strings.ContainsAny(c.Secrets.APIKey, " \t\r\n") {
		return errors.New("api_key must be a Resend API key, such as re_123")
	}
	var err error
	if c.Settings.From, err = address("from", c.Settings.From); err != nil {
		return err
	}
	c.Settings.To, err = recipients(c.Settings.To)
	return err
}

func (c *resendChannel) send(ctx context.Context, s *Service, e envelope) error {
	body, err := json.Marshal(map[string]any{"from": c.Settings.From, "to": c.Settings.To, "subject": e.Message.Title, "text": e.text()})
	if err != nil {
		return err
	}
	return s.post(ctx, strings.TrimSuffix(s.resend, "/")+"/emails", body, map[string]string{
		"Authorization":   "Bearer " + c.Secrets.APIKey,
		"Idempotency-Key": e.ID,
	})
}

type slackChannel struct {
	Settings struct{}
	Secrets  struct {
		WebhookURL string `json:"webhook_url"`
	}
}

func (c *slackChannel) fields() (any, any) { return &c.Settings, &c.Secrets }
func (c *slackChannel) parts() (any, any)  { return c.Settings, c.Secrets }

func (c *slackChannel) validate() error {
	var err error
	c.Secrets.WebhookURL, err = webURL("webhook_url", c.Secrets.WebhookURL)
	return err
}

// slackEscape escapes the three characters Slack's mrkdwn reserves.
var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func (c *slackChannel) send(ctx context.Context, s *Service, e envelope) error {
	lines := make([]string, len(e.Message.Lines))
	for i, l := range e.Message.Lines {
		lines[i] = slackEscape.Replace(l)
	}
	text := "*" + slackEscape.Replace(e.Message.Title) + "*"
	if len(lines) > 0 {
		text += "\n" + strings.Join(lines, "\n")
	}
	body, err := json.Marshal(map[string]any{
		"text": e.Message.Title,
		"blocks": []any{
			map[string]any{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": text}},
			map[string]any{"type": "context", "elements": []any{map[string]string{"type": "mrkdwn", "text": "<" + e.URL + "|Open in Byname>"}}},
		},
	})
	if err != nil {
		return err
	}
	return s.post(ctx, c.Secrets.WebhookURL, body, nil)
}

type webhookChannel struct {
	Settings struct {
		URL string `json:"url"`
	}
	Secrets struct {
		SigningSecret string `json:"signing_secret"`
	}
}

func (c *webhookChannel) fields() (any, any) { return &c.Settings, &c.Secrets }
func (c *webhookChannel) parts() (any, any)  { return c.Settings, c.Secrets }

func (c *webhookChannel) validate() error {
	var err error
	c.Settings.URL, err = webURL("url", c.Settings.URL)
	return err
}

// WebhookBody is the JSON a generic webhook receives.
type WebhookBody struct {
	ID        string          `json:"id"`
	Kind      Kind            `json:"kind"`
	Title     string          `json:"title"`
	Lines     []string        `json:"lines"`
	URL       string          `json:"url"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

func (c *webhookChannel) send(ctx context.Context, s *Service, e envelope) error {
	body, err := json.Marshal(WebhookBody{ID: e.ID, Kind: e.Kind, Title: e.Message.Title, Lines: e.Message.Lines, URL: e.URL, Payload: e.Payload, CreatedAt: e.CreatedAt})
	if err != nil {
		return err
	}
	return s.post(ctx, c.Settings.URL, body, map[string]string{
		"X-Byname-Event":    string(e.Kind),
		"X-Byname-Delivery": e.ID,
		SignatureHeader:     Sign(c.Secrets.SigningSecret, s.now(), body),
	})
}

// SignatureHeader carries a webhook's signature: "t=<unix seconds>,v1=<hex
// HMAC-SHA256 of "<t>.<body>" keyed with the channel's signing secret>".
// The timestamp is signed too, so a receiver that refuses old timestamps
// cannot be replayed a captured request.
const SignatureHeader = "X-Byname-Signature"

// Sign returns the SignatureHeader value for body sent at t.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	return "t=" + ts + ",v1=" + signature(secret, ts, body)
}

func signature(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a SignatureHeader value against body, and refuses a
// timestamp more than tolerance away from now. A receiver calls it with
// the raw body, before parsing it.
func Verify(secret, header string, body []byte, now time.Time, tolerance time.Duration) error {
	var ts, sig string
	for part := range strings.SplitSeq(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return errors.New("signature header is not t=<unix>,v1=<hex>")
	}
	if d := now.Sub(time.Unix(unix, 0)); d > tolerance || d < -tolerance {
		return fmt.Errorf("signature timestamp is %s away from now, outside %s", d.Round(time.Second), tolerance)
	}
	if !hmac.Equal([]byte(sig), []byte(signature(secret, ts, body))) {
		return errors.New("signature does not match the body")
	}
	return nil
}

const sendTimeout = 30 * time.Second

// post sends a JSON body and treats any 2xx as delivered.
func (s *Service) post(ctx context.Context, target string, body []byte, headers map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Byname")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := s.client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			// The URL can hold a secret (a Slack webhook's is one), so it
			// stays out of the delivery log.
			return uerr.Err
		}
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 == 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(snippet)))
}

func address(field, raw string) (string, error) {
	a, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%s must be an email address, such as Byname <alerts@example.com>", field)
	}
	switch {
	case a.Name == "":
		return a.Address, nil
	case plainName.MatchString(a.Name):
		// String() quotes every name; a plain one reads better bare and
		// parses back the same.
		return a.Name + " <" + a.Address + ">", nil
	}
	return a.String(), nil
}

var plainName = regexp.MustCompile(`^[A-Za-z0-9 ]+$`)

func recipients(in []string) ([]string, error) {
	out := []string{}
	for _, r := range in {
		if strings.TrimSpace(r) == "" {
			continue
		}
		a, err := mail.ParseAddress(strings.TrimSpace(r))
		if err != nil {
			return nil, fmt.Errorf("to: %q is not an email address", r)
		}
		if !slices.Contains(out, a.Address) {
			out = append(out, a.Address)
		}
	}
	if len(out) == 0 || len(out) > 20 {
		return nil, errors.New("to must list 1 to 20 email addresses")
	}
	return out, nil
}

// webURL accepts https, or http to a loopback host for a receiver on the
// same machine.
func webURL(field, raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("%s must be an https URL", field)
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		return "", fmt.Errorf("%s must use https unless it points at localhost", field)
	}
	return u.String(), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
