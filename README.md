# uplet

Checks if a URL is up and if the page actually exists (not just a soft
404). Available as a CLI or a local HTTP server.

## Requirements

- macOS (arm64 or amd64).

## Install

### From a release

Apple Silicon (arm64) example — swap `arm64` for `amd64` on Intel Macs:

```sh
curl -fL -o uplet https://github.com/ernilambar/uplet/releases/latest/download/uplet-darwin-arm64
sudo xattr -d com.apple.quarantine /usr/local/bin/uplet 2>/dev/null || true
chmod +x uplet
sudo mv uplet /usr/local/bin/
uplet --version
```

### From source

```sh
git clone https://github.com/ernilambar/uplet
cd uplet
go build -o build/uplet ./cmd/uplet   # -> build/uplet
go install ./cmd/uplet
```

## Usage

### `uplet check`

```
uplet check <url> [--json] [--timeout <duration>]
```

```console
$ uplet check https://example.com
OK  https://example.com
  status_code:    200
  response_time:  68ms
  reason:         ok (ok)
```

Exit codes (standard monitoring-plugin convention):

| Code | Meaning  | When                                             |
| ---- | -------- | ------------------------------------------------- |
| 0    | OK       | site up, page exists                              |
| 1    | WARNING  | site up, page missing (404, soft-404, other 4xx)  |
| 2    | CRITICAL | definitively down / unregistered / blocked        |
| 3    | UNKNOWN  | bad URL format, or indeterminate (timeout)        |

### `uplet serve`

```
uplet serve [--port <port>]   # default 54321, binds to 127.0.0.1 only
```

```console
$ curl -X POST 127.0.0.1:54321/v1/check \
    -H 'Content-Type: application/json' \
    -d '{"url":"https://example.com"}'
{"url":"https://example.com","valid_format":true,"site_up":true,"page_exists":true,"status_code":200,"response_time_ms":56,"reason":"ok","reason_code":"ok","attempts":2}
```

The response schema matches the `--json` output above and is additive-only
within `/v1` — a breaking change ships as `/v2/check`. `reason_code` is the
stable field to match on; `reason` is free text for humans. Possible values:
`ok`, `not_found`, `redirect_not_found`, `client_error`, `server_error`,
`network_error`, `dns_error`, `timeout`, `invalid_format`, `blocked_target`,
`too_large`, `tls_error`.

Server-level error responses (not from the checker itself):

| Status | Body                                 | Cause                          |
| ------ | ------------------------------------- | -------------------------------- |
| 400    | `{"error":"missing_url"}`            | malformed JSON or missing `url`  |
| 405    | `{"error":"method_not_allowed"}`     | non-POST request                 |
| 415    | `{"error":"unsupported_media_type"}` | missing/wrong `Content-Type`     |
| 429    | `{"error":"too_many_requests"}`      | concurrency cap exceeded         |

## Development

```sh
go build -o build/uplet ./cmd/uplet
go test ./...
go run ./cmd/uplet check https://example.com
```

### Manual Testing

Before opening a PR, verify the key commands against the real CLI:

```bash
go run ./cmd/uplet check https://example.com
go run ./cmd/uplet check https://example.com --json
go run ./cmd/uplet check https://example.com/this-page-does-not-exist
go run ./cmd/uplet check not-a-valid-url
go run ./cmd/uplet check https://this-domain-should-not-resolve-xyz123.invalid
go run ./cmd/uplet serve
curl -X POST 127.0.0.1:54321/v1/check -H 'Content-Type: application/json' -d '{"url":"https://example.com"}'
```

## Contributing

Contributions are welcome via pull request.

1. Fork the repo and create a branch off `main`.
2. Make your changes, keeping the SSRF-safety and bounds guarantees above intact.
3. Run `go test ./...` and ensure it passes.
4. Open a PR describing the change and why it's needed.

For bugs or feature requests, open an [issue](https://github.com/ernilambar/uplet/issues).

## Release

Tags must be prefixed with `v` (e.g. `v1.0.1`).

```bash
git tag v1.0.1
git push origin v1.0.1
```

## License

[MIT](http://opensource.org/licenses/MIT) © 2026 [Nilambar Sharma](https://www.nilambar.net)
