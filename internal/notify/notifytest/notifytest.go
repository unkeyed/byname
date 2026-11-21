// Package notifytest fakes the services notification channels send to: an
// SMTP server, Resend's HTTP API, and an HTTP receiver for Slack incoming
// webhooks and generic webhooks that records every request and checks a
// webhook's signature. Each records what it got, so a test can assert on
// exactly what was sent.
package notifytest

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/unkeyed/byname/internal/notify"
)

// Mail is one message the SMTP server accepted.
type Mail struct {
	From    string
	To      []string
	Subject string
	// Body is the decoded text body.
	Body string
	// User is who signed in with AUTH PLAIN, empty without AUTH.
	User string
	TLS  bool
}

// SMTPServer is an SMTP sink. It speaks EHLO, STARTTLS (when it has a
// certificate), AUTH PLAIN, MAIL, RCPT, DATA, RSET, NOOP and QUIT.
type SMTPServer struct {
	Addr string
	// Users is who may sign in, by username to password. Empty accepts
	// mail without AUTH.
	Users map[string]string

	ln       net.Listener
	tls      *tls.Config
	implicit bool
	mu       sync.Mutex
	mail     []Mail
}

// SMTPMode is how a client reaches the server.
type SMTPMode string

const (
	Plain    SMTPMode = "none"
	StartTLS SMTPMode = "starttls"
	Implicit SMTPMode = "tls"
)

// StartSMTP serves SMTP on ln in mode. Modes other than Plain use a
// self-signed certificate for localhost and 127.0.0.1, which RootCAs
// trusts.
func StartSMTP(ln net.Listener, mode SMTPMode) (*SMTPServer, error) {
	s := &SMTPServer{Addr: ln.Addr().String(), Users: map[string]string{}}
	if mode != Plain {
		cfg, err := selfSigned()
		if err != nil {
			return nil, err
		}
		s.tls = cfg
	}
	if mode == Implicit {
		ln = tls.NewListener(ln, s.tls)
		s.implicit = true
	}
	s.ln = ln
	go s.serve()
	return s, nil
}

// NewSMTP starts an SMTP sink on a free local port that stops when the
// test ends.
func NewSMTP(t testing.TB, mode SMTPMode) *SMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := StartSMTP(ln, mode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func (s *SMTPServer) Close() { _ = s.ln.Close() }

// Host and Port split Addr.
func (s *SMTPServer) Host() string { h, _, _ := net.SplitHostPort(s.Addr); return h }
func (s *SMTPServer) Port() int {
	_, p, _ := net.SplitHostPort(s.Addr)
	var n int
	fmt.Sscan(p, &n)
	return n
}

// RootCAs trusts the server's certificate.
func (s *SMTPServer) RootCAs() *x509.CertPool {
	pool := x509.NewCertPool()
	if s.tls != nil {
		pool.AddCert(s.tls.Certificates[0].Leaf)
	}
	return pool
}

// Mail returns every message accepted so far.
func (s *SMTPServer) Mail() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.mail...)
}

func (s *SMTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(conn)
	}
}

func (s *SMTPServer) session(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Minute))
	r, w := bufio.NewReader(conn), bufio.NewWriter(conn)
	reply := func(format string, args ...any) {
		fmt.Fprintf(w, format+"\r\n", args...)
		_ = w.Flush()
	}
	secure := s.implicit
	var m Mail
	reply("220 notifytest ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			var ext []string
			if s.tls != nil && !secure {
				ext = append(ext, "STARTTLS")
			}
			if len(s.Users) > 0 {
				ext = append(ext, "AUTH PLAIN")
			}
			if len(ext) == 0 {
				reply("250 notifytest")
				continue
			}
			fmt.Fprintf(w, "250-notifytest\r\n")
			for i, e := range ext {
				sep := "-"
				if i == len(ext)-1 {
					sep = " "
				}
				fmt.Fprintf(w, "250%s%s\r\n", sep, e)
			}
			_ = w.Flush()
		case "STARTTLS":
			if s.tls == nil || secure {
				reply("502 not offered")
				continue
			}
			reply("220 go ahead")
			tc := tls.Server(conn, s.tls)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, secure = tc, true
			r, w = bufio.NewReader(tc), bufio.NewWriter(tc)
		case "AUTH":
			mech, initial, _ := strings.Cut(arg, " ")
			raw, err := base64.StdEncoding.DecodeString(initial)
			parts := strings.Split(string(raw), "\x00")
			if !strings.EqualFold(mech, "PLAIN") || err != nil || len(parts) != 3 || s.Users[parts[1]] != parts[2] || parts[1] == "" {
				reply("535 authentication failed")
				continue
			}
			m.User = parts[1]
			reply("235 ok")
		case "MAIL":
			if len(s.Users) > 0 && m.User == "" {
				reply("530 sign in first")
				continue
			}
			m.From = pathArg(arg)
			reply("250 ok")
		case "RCPT":
			m.To = append(m.To, pathArg(arg))
			reply("250 ok")
		case "DATA":
			reply("354 end with .")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			m.TLS = secure
			s.record(m, b.String())
			m = Mail{User: m.User}
			reply("250 queued")
		case "RSET":
			m = Mail{User: m.User}
			reply("250 ok")
		case "NOOP":
			reply("250 ok")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unknown command")
		}
	}
}

// pathArg reads the address out of "FROM:<a@example.com> SIZE=1".
func pathArg(arg string) string {
	_, path, _ := strings.Cut(arg, ":")
	path, _, _ = strings.Cut(strings.TrimSpace(path), " ")
	return strings.Trim(path, "<>")
}

func (s *SMTPServer) record(m Mail, data string) {
	if msg, err := mail.ReadMessage(strings.NewReader(data)); err == nil {
		var dec mime.WordDecoder
		m.Subject, _ = dec.DecodeHeader(msg.Header.Get("Subject"))
		body, _ := io.ReadAll(msg.Body)
		if strings.EqualFold(msg.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
			body, _ = io.ReadAll(quotedprintable.NewReader(strings.NewReader(string(body))))
		}
		m.Body = strings.ReplaceAll(string(body), "\r\n", "\n")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mail = append(s.mail, m)
}

func selfSigned() (*tls.Config, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "notifytest"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}}}, nil
}

// Email is one POST /emails the Resend fake accepted, in Resend's request
// shape.
type Email struct {
	From           string   `json:"from"`
	To             []string `json:"to"`
	Subject        string   `json:"subject"`
	Text           string   `json:"text"`
	APIKey         string   `json:"-"`
	IdempotencyKey string   `json:"-"`
}

// Resend fakes Resend's POST /emails: it wants Authorization: Bearer
// APIKey and answers {"id": "..."}, or Resend's error shape
// {"statusCode", "name", "message"} for a wrong key.
type Resend struct {
	mu     sync.Mutex
	apiKey string
	emails []Email
}

// NewResend starts a Resend fake that stops when the test ends.
func NewResend(t testing.TB, apiKey string) (*Resend, *httptest.Server) {
	r := &Resend{apiKey: apiKey}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return r, srv
}

func (f *Resend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != "/emails" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 404, "name": "not_found", "message": "The requested endpoint does not exist."})
		return
	}
	f.mu.Lock()
	key := f.apiKey
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+key {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 401, "name": "validation_error", "message": "API key is invalid"})
		return
	}
	var e Email
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 422, "name": "validation_error", "message": err.Error()})
		return
	}
	e.APIKey = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	e.IdempotencyKey = r.Header.Get("Idempotency-Key")
	f.mu.Lock()
	f.emails = append(f.emails, e)
	n := len(f.emails)
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]string{"id": fmt.Sprintf("email_%d", n)})
}

// SetAPIKey changes the key the fake accepts, as rotating it at Resend does.
func (f *Resend) SetAPIKey(k string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apiKey = k
}

// Emails returns every email accepted so far.
func (f *Resend) Emails() []Email {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Email(nil), f.emails...)
}

// Request is one request a Receiver got.
type Request struct {
	Header http.Header
	Body   []byte
	// SignatureErr is why a webhook's signature did not verify, nil when it
	// did. Only set when the receiver has a secret.
	SignatureErr error
}

// Receiver records every POST. With a Secret it verifies
// X-Byname-Signature the way a webhook receiver should, and refuses a bad
// one with 401. Fail, when set, picks a status to answer instead of 200.
type Receiver struct {
	mu     sync.Mutex
	secret string
	fail   func(n int) int
	reqs   []Request
}

// NewReceiver starts a receiver that stops when the test ends.
func NewReceiver(t testing.TB) (*Receiver, *httptest.Server) {
	r := &Receiver{}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return r, srv
}

// SetSecret sets the signing secret to verify against.
func (r *Receiver) SetSecret(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secret = s
}

// Fail answers the nth request (from 1) with the status f returns, or 200
// when it returns 0.
func (r *Receiver) Fail(f func(n int) int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = f
}

func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	rec := Request{Header: req.Header.Clone(), Body: body}
	r.mu.Lock()
	if r.secret != "" {
		rec.SignatureErr = notify.Verify(r.secret, req.Header.Get(notify.SignatureHeader), body, time.Now(), 5*time.Minute)
	}
	r.reqs = append(r.reqs, rec)
	status := 0
	if r.fail != nil {
		status = r.fail(len(r.reqs))
	}
	r.mu.Unlock()
	switch {
	case status != 0:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "fake failure")
	case rec.SignatureErr != nil:
		http.Error(w, rec.SignatureErr.Error(), http.StatusUnauthorized)
	default:
		_, _ = io.WriteString(w, "ok")
	}
}

// Requests returns every request received so far.
func (r *Receiver) Requests() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Request(nil), r.reqs...)
}
