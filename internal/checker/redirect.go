package checker

import (
	"net/url"
	"strings"
)

// redirectIsSoftNotFound reports whether following original to final looks
// like a "redirect to somewhere else because the page is gone" rather than
// a transparent redirect (scheme upgrade, www add/drop, trailing-slash
// normalization, locale-prefix, or a root request landing anywhere).
// Conservative by design: it only flags a specific, non-root path
// redirecting to a genuinely different, non-locale-equivalent path.
func redirectIsSoftNotFound(original, final *url.URL) bool {
	origHost := normalizeHost(original.Hostname())
	finalHost := normalizeHost(final.Hostname())
	if origHost != finalHost {
		return true
	}

	origPath := normalizePath(original.EscapedPath())
	finalPath := normalizePath(final.EscapedPath())
	if origPath == finalPath {
		return false
	}
	if origPath == "/" {
		return false
	}
	return stripLocalePrefix(origPath) != stripLocalePrefix(finalPath)
}

func normalizeHost(h string) string {
	return strings.TrimPrefix(strings.ToLower(h), "www.")
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
		if p == "" {
			p = "/"
		}
	}
	return p
}

// stripLocalePrefix removes a leading short alphabetic (optionally
// hyphenated) path segment such as "/en" or "/en-us", treating it as a
// locale marker rather than meaningful path content.
func stripLocalePrefix(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	seg, rest, hasRest := strings.Cut(trimmed, "/")
	if !isLocaleSegment(seg) {
		return path
	}
	if hasRest {
		return "/" + rest
	}
	return "/"
}

func isLocaleSegment(seg string) bool {
	if len(seg) < 2 || len(seg) > 5 {
		return false
	}
	for _, r := range seg {
		if r == '-' {
			continue
		}
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}
