package providertest

import (
	"io"
	"net/http"
	"testing"

	"github.com/unkeyed/byname/internal/engines"
)

func TestDeniesAWrongKeyAndRecordsIt(t *testing.T) {
	s := New(t)
	req, err := http.NewRequest(http.MethodGet, s.BaseURL(engines.CredentialAnthropic)+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("x-api-key", "wrong")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", resp.StatusCode)
	}
	calls := s.Calls()
	if len(calls) != 1 || calls[0].Kind != engines.CredentialAnthropic || calls[0].Path != "/v1/models" || calls[0].Authorized {
		t.Errorf("calls %+v", calls)
	}
	s.Reset()
	if len(s.Calls()) != 0 {
		t.Error("Reset kept calls")
	}
}

func TestRespondReplacesARoute(t *testing.T) {
	s := New(t)
	s.Respond(engines.CredentialGateway, http.MethodGet, "/models", http.StatusServiceUnavailable, []byte(`{"error":{"message":"busy"}}`))
	req, err := http.NewRequest(http.MethodGet, s.BaseURL(engines.CredentialGateway)+"/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+Secret(engines.CredentialGateway))
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable || string(body) != `{"error":{"message":"busy"}}` {
		t.Errorf("got %d %s", resp.StatusCode, body)
	}
}
