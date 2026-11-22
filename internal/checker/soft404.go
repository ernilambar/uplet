package checker

import (
	"errors"
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

// checkSoft404 reads up to MaxBodyBytes of the response body and reports
// whether it looks like a soft 404. A body larger than the cap is not
// skipped: its first MaxBodyBytes are inspected (not-found markers almost
// always appear early, in <title> or the main heading) and tooLarge
// reports that the rest went unread. err reports a failed body read, in
// which case the other results are meaningless. HEAD responses have no
// body to inspect and are treated as existing.
func (c *Checker) checkSoft404(resp *http.Response) (exists bool, tooLarge bool, err error) {
	if resp.Request.Method == http.MethodHead {
		return true, false, nil
	}

	limit := c.MaxBodyBytes
	buf := make([]byte, limit+1)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, false, err
	}
	if int64(n) > limit {
		tooLarge = true
		n = int(limit)
	}

	body := strings.ToLower(string(buf[:n]))
	for _, pat := range softNotFoundPatterns {
		if strings.Contains(body, pat) {
			return false, tooLarge, nil
		}
	}
	return true, tooLarge, nil
}
