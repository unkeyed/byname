DATABASE_URL ?= postgres://byname:byname@localhost:5432/byname?sslmode=disable
BASE_URL ?= http://localhost:8080
GO_TEST_SKIP ?=
E2E_SETUP_TOKEN ?=

.PHONY: dev test lint build web e2e

# make dev keeps a local ENCRYPTION_KEY in .byname-dev-key (gitignored), so
# secrets stored while developing stay readable across restarts.
dev:
	docker compose up -d --wait db
	@test -s .byname-dev-key || openssl rand -base64 32 > .byname-dev-key
	DATABASE_URL='$(DATABASE_URL)' ENCRYPTION_KEY="$$(cat .byname-dev-key)" go run ./cmd/byname

test:
	go test -skip '$(GO_TEST_SKIP)' ./...

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run: gofmt -w ."; exit 1; }
	go vet ./...
	go vet -tags e2e ./e2e/
	cd web && npm test

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -o bin/byname ./cmd/byname

e2e:
	BASE_URL='$(BASE_URL)' E2E_SETUP_TOKEN='$(E2E_SETUP_TOKEN)' go test -tags e2e -count=1 -v ./e2e/
