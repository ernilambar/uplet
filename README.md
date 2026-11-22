# uplet

CLI tool to check that a URL is up and the page really exists.

## Features

- **Catches fake "page found" responses.** Some sites return success for
  a page that doesn't exist. uplet reports it as missing anyway.
- **Clear results.** Plain text for reading, `--json` for scripts, and
  exit codes that work with monitoring tools as-is.
- **One file, nothing to set up.** A single binary with no dependencies
  and no config.

## Requirements

- macOS 15 or later (Apple Silicon).

## Install

```sh
brew tap ernilambar/tap
brew trust ernilambar/tap
brew install ernilambar/tap/uplet
```

For a release binary or a source build, see [docs/install.md](docs/install.md).

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

The server has no authentication. Its only access control is binding to
`127.0.0.1` and rejecting requests whose `Host` isn't `127.0.0.1`. Don't
put it behind a reverse proxy or forward the port: anyone who can reach it
can make uplet send HTTP requests on your behalf.

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
`too_large`, `tls_error`, `too_many_redirects`.

Server-level error responses (not from the checker itself):

| Status | Body                                 | Cause                          |
| ------ | ------------------------------------- | -------------------------------- |
| 400    | `{"error":"missing_url"}`            | malformed JSON or missing `url`  |
| 403    | `{"error":"forbidden_host"}`         | `Host` header isn't `127.0.0.1`  |
| 405    | `{"error":"method_not_allowed"}`     | non-POST request                 |
| 415    | `{"error":"unsupported_media_type"}` | missing/wrong `Content-Type`     |
| 429    | `{"error":"too_many_requests"}`      | concurrency cap exceeded         |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) © [Nilambar Sharma](https://www.nilambar.net)
