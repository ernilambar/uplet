# uplet Changelog

## Unreleased
- Drop Intel (darwin/amd64) builds.
- Require macOS 15 or later.
- Fix soft-404 detection missing bodies that arrive after the response headers.
- Block more SSRF targets: `::`, CGNAT, reserved, multicast, Teredo, and private IPv4 embedded in NAT64/6to4.
- Fix `serve` leaking connections after each check.
- Reject `serve` requests whose `Host` isn't `127.0.0.1` to defend against DNS rebinding.
- Fix short paths like `/about` being treated as locale prefixes, hiding redirects to the homepage.
- Stop flagging redirects to another host unless they land on that site's root.
- Inspect the start of bodies over the 64KB cap instead of skipping soft-404 detection.
- Send an identifying `User-Agent` instead of Go's default.
- Try every resolved address before reporting a host down.
- Add `too_many_redirects` reason code; redirect loops are no longer retried.
- Keep `site_up` true when headers arrive but the body read fails.
- Reject a zero or negative `--timeout`.
- Add a read-header timeout to `serve`.
- Publish `SHA256SUMS` with release binaries.

## 1.0.0 - 2026-08-03
- Initial release
