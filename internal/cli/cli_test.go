package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ernilambar/uplet/internal/checker"
)

func TestRunCheck_InvalidFormatIsUnknown(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runCheck([]string{"not-a-url"}, &out, &errOut)
	if code != exitUnknown {
		t.Fatalf("exit code = %d, want %d", code, exitUnknown)
	}
	if !strings.Contains(out.String(), "UNKNOWN") {
		t.Fatalf("output missing UNKNOWN indicator: %s", out.String())
	}
}

func TestRunCheck_MissingURL(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runCheck([]string{}, &out, &errOut)
	if code != exitUnknown {
		t.Fatalf("exit code = %d, want %d", code, exitUnknown)
	}
	if errOut.Len() == 0 {
		t.Fatal("expected an error message on stderr")
	}
}

func TestRunCheck_InvalidTimeoutFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runCheck([]string{"--timeout", "not-a-duration", "http://example.com"}, &out, &errOut)
	if code != exitUnknown {
		t.Fatalf("exit code = %d, want %d", code, exitUnknown)
	}
}

func TestRunCheck_JSONOutput(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runCheck([]string{"--json", "not-a-url"}, &out, &errOut)
	if code != exitUnknown {
		t.Fatalf("exit code = %d, want %d; stderr=%s", code, exitUnknown, errOut.String())
	}

	var res checker.Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if res.ReasonCode != string(checker.ReasonInvalidFormat) {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// The production blocklist rejects loopback, so httptest.Server (which
// listens on 127.0.0.1) exercises the blocked_target/CRITICAL path end to
// end — flag parsing, the real checker.Check call, and output formatting.
// The OK/WARNING status-mapping logic itself is covered by the checker
// package's own tests; TestExitCodeFor below covers the exit-code mapping.
func TestRunCheck_BlockedTargetIsCritical(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	var out, errOut bytes.Buffer
	code := runCheck([]string{ts.URL}, &out, &errOut)
	if code != exitCritical {
		t.Fatalf("exit code = %d, want %d; stderr=%s", code, exitCritical, errOut.String())
	}
	if !strings.Contains(out.String(), "CRITICAL") || !strings.Contains(out.String(), string(checker.ReasonBlockedTarget)) {
		t.Fatalf("output missing CRITICAL/blocked_target: %s", out.String())
	}
}

func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		code checker.ReasonCode
		want int
	}{
		{checker.ReasonOK, exitOK},
		{checker.ReasonTooLarge, exitOK},
		{checker.ReasonNotFound, exitWarning},
		{checker.ReasonRedirectNotFound, exitWarning},
		{checker.ReasonClientError, exitWarning},
		{checker.ReasonInvalidFormat, exitUnknown},
		{checker.ReasonTimeout, exitUnknown},
		{checker.ReasonServerError, exitCritical},
		{checker.ReasonNetworkError, exitCritical},
		{checker.ReasonDNSError, exitCritical},
		{checker.ReasonBlockedTarget, exitCritical},
		{checker.ReasonTLSError, exitCritical},
	}
	for _, c := range cases {
		got := exitCodeFor(checker.Result{ReasonCode: string(c.code)})
		if got != c.want {
			t.Errorf("exitCodeFor(%q) = %d, want %d", c.code, got, c.want)
		}
	}
}
