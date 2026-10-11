#!/usr/bin/env bash
set -euo pipefail

echo "==> Installing Go developer tooling"
GOLANGCI_VERSION="v2.14.0"
MIGRATE_VERSION="v4.20.1"

# golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/${GOLANGCI_VERSION}/install.sh \
  | sh -s -- -b "$(go env GOPATH)/bin" "${GOLANGCI_VERSION}"

# golang-migrate CLI (postgres driver)
go install -tags 'postgres' "github.com/golang-migrate/migrate/v4/cmd/migrate@${MIGRATE_VERSION}"

echo "==> Downloading Go module dependencies"
(cd backend && go mod download || true)

echo "==> Installing frontend dependencies"
(cd frontend && npm ci)

echo "==> Dev container ready. Run 'make up' to start the stack."
