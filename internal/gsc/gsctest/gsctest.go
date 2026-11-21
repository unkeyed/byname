package gsctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"testing"
)

// Server is a running Fake.
type Server struct {
	*Fake
	URL string
}

// New starts a fake Google for one test, with OAuth client "test-client"
// and secret "test-secret".
func New(t testing.TB) *Server {
	t.Helper()
	f := NewFake("test-client", "test-secret")
	srv := httptest.NewServer(f.Handler())
	t.Cleanup(srv.Close)
	return &Server{Fake: f, URL: srv.URL}
}

// ServiceAccountKey makes a service account the fake accepts and returns
// its JSON key in the format Google downloads.
func (s *Server) ServiceAccountKey(t testing.TB, email string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	s.ServiceAccounts[email] = &key.PublicKey
	b, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "byname-test",
		"private_key_id": "0123456789abcdef",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   email,
		"client_id":      "100000000000000000001",
		"token_uri":      "https://oauth2.googleapis.com/token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
