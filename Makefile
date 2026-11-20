DATABASE_URL ?= postgres://keyring:keyring@localhost:5432/keyring?sslmode=disable
BASE_URL ?= http://localhost:8080
GO_TEST_SKIP ?=
E2E_SETUP_TOKEN ?=

.PHONY: dev test lint build web e2e

# make dev keeps a local ENCRYPTION_KEY in .keyring-dev-key (gitignored), so
# secrets stored while developing stay readable across restarts.
dev:
	docker compose up -d --wait db
	@test -s .keyring-dev-key || openssl rand -base64 32 > .keyring-dev-key
	DATABASE_URL='$(DATABASE_URL)' ENCRYPTION_KEY="$$(cat .keyring-dev-key)" go run ./cmd/keyring

test:
	go test -skip '$(GO_TEST_SKIP)' ./...

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run: gofmt -w ."; exit 1; }
	go vet ./...
	go vet -tags e2e ./e2e/

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -o bin/keyring ./cmd/keyring

e2e:
	BASE_URL='$(BASE_URL)' E2E_SETUP_TOKEN='$(E2E_SETUP_TOKEN)' go test -tags e2e -count=1 -v ./e2e/
