// Package e2e checks Byname against a running instance, over HTTP only, the
// way a person and their agents reach it:
//
//   - GET /healthz answers 200, and GET /v1/openapi.yaml serves the spec
//     without credentials.
//   - The setup link creates the first account, an admin with every scope.
//   - An API key reaches GET /v1/status only with results:read, never an
//     admin route, and can revoke itself.
//   - The MCP server at /mcp refuses API keys, and an MCP client that signs
//     in with OAuth as the person lists its tools and calls status. BASE_URL
//     must be Byname's PUBLIC_URL, since the client checks the resource it
//     discovers against the URL it connects to.
//   - A password alone never sets a session, and a used setup link is dead.
//
// Run it against a fresh compose stack, passing the token of the setup link
// Byname logs at start:
//
//	docker compose up -d --build --wait
//	make e2e E2E_SETUP_TOKEN=<token from the setup link>
//
// The tests sign in the way a person does, with no test-only backdoor:
// create the first account through the setup link with a software passkey
// (internal/authtest), then create keys with POST /v1/keys. The passkey is
// saved to E2E_PASSKEY_FILE (default: a file in the temp directory named
// after BASE_URL), so later runs sign in with it and need no token.
//
// Environment: BASE_URL (default http://localhost:8080), E2E_PUBLIC_URL (the
// instance's PUBLIC_URL, default BASE_URL), E2E_SETUP_TOKEN,
// E2E_PASSKEY_FILE and E2E_ADMIN_EMAIL. The tests carry the e2e build tag,
// so `go test ./...` skips this package.
package e2e
