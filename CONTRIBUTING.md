# Contributing

Contributions are welcome via pull request. For bugs or feature requests,
open an [issue](https://github.com/ernilambar/uplet/issues).

## Workflow

1. Fork the repo and create a branch off `main`.
2. Make your change, keeping the rules below intact.
3. Pass the quality gate and the manual checks.
4. Add a changelog entry.
5. Open a PR describing the change and why it's needed.

## Rules

- **SSRF safety is non-negotiable.** Every resolved IP — initial and every
  redirect hop — is validated against the blocklist before dialing. Don't
  bypass, cache, or skip it.
- **Reason codes are a contract.** Add new `reason_code` values; never
  repurpose existing ones. `/v1/check` responses only gain fields.
- **Exit codes are a contract.** `0 OK / 1 WARNING / 2 CRITICAL / 3 UNKNOWN`
  — downstream alerting depends on this mapping.

## Development

Requires Go 1.27.3. Standard library only — no dependencies to install.

```sh
go build -o build/uplet ./cmd/uplet
go run ./cmd/uplet check https://example.com
```

### Quality gate

Each must exit 0:

```sh
go vet ./...
go test ./...
go build -o build/uplet ./cmd/uplet
```

### Manual testing

Verify the key commands against the real CLI:

```sh
go run ./cmd/uplet check https://example.com
go run ./cmd/uplet check https://example.com --json
go run ./cmd/uplet check https://example.com/this-page-does-not-exist
go run ./cmd/uplet check not-a-valid-url
go run ./cmd/uplet check https://this-domain-should-not-resolve-xyz123.invalid
go run ./cmd/uplet serve
curl -X POST 127.0.0.1:54321/v1/check -H 'Content-Type: application/json' -d '{"url":"https://example.com"}'
```

## Changelog

Add user-facing changes to `CHANGELOG.md` under the `## X.X.X - YYYY-MM-DD`
block (create it at the top if missing). Each bullet starts with `Added:`,
`Changed:`, or `Fixed:`, grouped in that order. Validate with:

```sh
npx @nilambar/flatlog validate
```

## Release

Maintainers only. Replace the changelog placeholder with
`## <version> - <date>`, then tag. Tags must be prefixed with `v`
(e.g. `v1.0.1`); the tag sets the binary's version and triggers the
release workflow.

```sh
git tag v1.0.1
git push origin v1.0.1
```
