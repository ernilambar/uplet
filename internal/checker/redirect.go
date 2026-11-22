package checker

import (
	"net/url"
	"strings"
)

// redirectIsSoftNotFound reports whether following original to final looks
// like a "redirect to somewhere else because the page is gone" rather than
// a transparent redirect (scheme upgrade, www add/drop, trailing-slash
// normalization, locale-prefix, or a root request landing anywhere).
// Conservative by design: on the same host it flags a specific, non-root
// path redirecting to a genuinely different, non-locale-equivalent path.
// Across hosts it flags only a non-root path collapsing to the other
// site's root: domain moves and URL shorteners legitimately change host
// and path, so anything else is given the benefit of the doubt.
func redirectIsSoftNotFound(original, final *url.URL) bool {
	origPath := normalizePath(original.EscapedPath())
	if origPath == "/" {
		return false
	}
	origCore := stripLocalePrefix(origPath)
	finalCore := stripLocalePrefix(normalizePath(final.EscapedPath()))

	if normalizeHost(original.Hostname()) != normalizeHost(final.Hostname()) {
		return origCore != "/" && finalCore == "/"
	}
	return origCore != finalCore
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

// stripLocalePrefix removes a leading locale path segment such as "/en",
// "/en-us" or "/zh-Hant", treating it as a locale marker rather than
// meaningful path content.
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

// isLocaleSegment reports whether seg looks like a locale tag: a two-letter
// language code, optionally followed by "-" or "_" and a 2–4 letter region
// or script subtag ("en", "en-US", "pt_BR", "zh-Hant"). Plain short words
// like "about" or "blog" must not match, or redirects such as /about → /
// would be mistaken for locale normalization.
func isLocaleSegment(seg string) bool {
	lang, sub, hasSub := strings.Cut(seg, "-")
	if !hasSub {
		lang, sub, hasSub = strings.Cut(seg, "_")
	}
	if len(lang) != 2 || !isASCIILetters(lang) {
		return false
	}
	if !hasSub {
		return true
	}
	return len(sub) >= 2 && len(sub) <= 4 && isASCIILetters(sub)
}

func isASCIILetters(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}
