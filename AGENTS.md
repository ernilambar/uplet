# AGENTS.md

## Overview

uplet is a Go CLI tool that checks whether a URL is up and the page truly exists — it catches soft-404s and fake success responses that naive checks miss. Single binary, stdlib-only (Go 1.27.3), no external dependencies.

## Setup

- Install Go 1.27.3 (matches CI and go.mod).
- No `go mod download` needed — the module uses only the standard library.
- Clone and build: `go build -o build/uplet ./cmd/uplet`.

## Commands

- Build binary: `go build -o build/uplet ./cmd/uplet`
- Release build: `go build -trimpath -ldflags="-s -w" -o uplet ./cmd/uplet`
- Run all tests: `go test ./...`
- Vet: `go vet ./...`
- Run check command: `go run ./cmd/uplet check https://example.com`
- Run check as JSON: `go run ./cmd/uplet check https://example.com --json`
- Run server: `go run ./cmd/uplet serve`

## Conventions

- **SSRF safety is non-negotiable.** The checker resolves and validates every IP (initial + every redirect hop) against a blocklist before dialing. Do not bypass, cache, or skip this validation.
- **Tabs for Go, spaces for JSON/YAML/MD.** Enforced by `.editorconfig` — `indent_style = tab` for `*.go`, `indent_style = space` for `*.json`, `*.yml`, `*.md`.
- **Stable reason codes over free text.** `ReasonCode` (e.g. `ok`, `not_found`, `dns_error`) is the contract; `Reason` is human-readable detail. Add new codes, don't repurpose existing ones.
- **Loopback-only server.** `serve` binds to `127.0.0.1` only — never `0.0.0.0`, never `localhost`. Requests whose `Host` isn't `127.0.0.1` are rejected (DNS-rebinding defense). Together these are the only access control.
- **Exit codes are a monitoring contract.** `0 OK / 1 WARNING / 2 CRITICAL / 3 UNKNOWN`. Preserve this mapping; changing it breaks downstream alerting.
- **API versioning is additive.** `/v1/check` responses only gain fields; breaking changes ship as `/v2/check`.

## Release

- **The version lives only in `CHANGELOG.md`.** No source file holds it — the binary's version is injected from the git tag at build time (`-X .../internal/cli.version=<tag>` in `.github/workflows/release.yml`).
- **To bump:** replace the `## X.X.X - YYYY-MM-DD` placeholder in `CHANGELOG.md` with `## <version> - <today's date>` (SemVer, no `v` prefix). If the tagging example in `CONTRIBUTING.md` should show the new version, update it too.
- **Changelog must pass `npx @nilambar/flatlog validate`.** Bullets start with `Added:`, `Changed:`, or `Fixed:`, grouped in that order.
- **Tags are `v`-prefixed** (e.g. `v1.0.1`). A `v*` tag triggers the release workflow, which builds the darwin-arm64 binary and publishes it with `SHA256SUMS`.

## Quality Gate

Run these in order and confirm each exits 0 before declaring work done:

1. `go vet ./...`
2. `go test ./...`
3. `go build -o build/uplet ./cmd/uplet`
