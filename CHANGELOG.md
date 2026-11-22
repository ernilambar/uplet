# uplet Changelog

## Unreleased
- Drop Intel (darwin/amd64) builds.
- Require macOS 15 or later.
- Fix soft-404 detection missing bodies that arrive after the response headers.
- Block more SSRF targets: `::`, CGNAT, reserved, multicast, Teredo, and private IPv4 embedded in NAT64/6to4.
- Fix `serve` leaking connections after each check.
- Reject `serve` requests whose `Host` isn't `127.0.0.1` to defend against DNS rebinding.

## 1.0.0 - 2026-08-03
- Initial release
