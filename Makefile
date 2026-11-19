DATABASE_URL ?= postgres://uncrm:uncrm@localhost:5432/uncrm?sslmode=disable
BASE_URL ?= http://localhost:8080
GO_TEST_SKIP ?= TestEveryProviderHasFixtures

.PHONY: dev test lint build web e2e

dev:
	docker compose up -d --wait db
	DATABASE_URL='$(DATABASE_URL)' ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=uncrm-dev-password go run ./cmd/uncrm

test:
	go test -skip '$(GO_TEST_SKIP)' ./...

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run: gofmt -w ."; exit 1; }
	go vet ./...
	go vet -tags e2e ./e2e/

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -o bin/uncrm ./cmd/uncrm

e2e:
	BASE_URL='$(BASE_URL)' go test -tags e2e -count=1 -v ./e2e/
