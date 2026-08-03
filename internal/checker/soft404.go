package checker

import (
	"io"
	"net/http"
	"strings"
)

// softNotFoundPatterns are common phrases used by pages that return 200 OK
// while telling the visitor the resource doesn't exist. Best-effort and
// heuristic, not authoritative.
var softNotFoundPatterns = []string{
	"page not found",
	"404 not found",
	"404 error",
	"page you are looking for",
	"page you requested",
	"doesn't exist",
	"does not exist",
	"no longer available",
	"content not found",
	"we couldn't find",
	"we could not find",
	"sorry, this page",
}

// checkSoft404 reads a capped slice of the response body and reports
// whether it looks like a soft 404. tooLarge reports the body exceeded
// MaxBodyBytes; exists is meaningless when tooLarge is true. HEAD
// responses have no body to inspect and are treated as existing.
func (c *Checker) checkSoft404(resp *http.Response) (exists bool, tooLarge bool) {
	if resp.Request.Method == http.MethodHead {
		return true, false
	}

	limit := c.MaxBodyBytes
	buf := make([]byte, limit+1)
	n, _ := io.ReadFull(resp.Body, buf)
	if int64(n) > limit {
		return false, true
	}

	body := strings.ToLower(string(buf[:n]))
	for _, pat := range softNotFoundPatterns {
		if strings.Contains(body, pat) {
			return false, false
		}
	}
	return true, false
}
