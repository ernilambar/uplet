package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ernilambar/uplet/internal/checker"
)

func TestRunCheck_InvalidFormatIsUnknown(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	var out, errOut bytes.Buffer
	code := runCheck([]string{"--timeout", "not-a-duration", "http://example.com"}, &out, &errOut)
	if code != exitUnknown {
		t.Fatalf("exit code = %d, want %d", code, exitUnknown)
	}
}

func TestRunCheck_JSONOutput(t *testing.T) {
	t.Parallel()
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

// TestRunCheck_JSONFlagAfterURL is the regression test for reorderFlags:
// flag.FlagSet.Parse stops at the first positional argument, so without
// reordering "check <url> --json" would drop --json and print human output.
func TestRunCheck_JSONFlagAfterURL(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	code := runCheck([]string{"not-a-url", "--json"}, &out, &errOut)
	if code != exitUnknown {
		t.Fatalf("exit code = %d, want %d; stderr=%s", code, exitUnknown, errOut.String())
	}

	var res checker.Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("expected JSON output when --json follows the URL: %v\n%s", err, out.String())
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
	t.Parallel()
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

func TestReorderFlags(t *testing.T) {
	t.Parallel()

	newFS := func() *flag.FlagSet {
		fs := flag.NewFlagSet("check", flag.ContinueOnError)
		fs.Bool("json", false, "")
		fs.Duration("timeout", 0, "")
		return fs
	}

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"flags already first", []string{"--json", "u"}, []string{"--json", "u"}},
		{"bool flag after url", []string{"u", "--json"}, []string{"--json", "u"}},
		{"value flag after url", []string{"u", "--timeout", "5s"}, []string{"--timeout", "5s", "u"}},
		{"inline value flag", []string{"u", "--timeout=5s"}, []string{"--timeout=5s", "u"}},
		{"bool then value flag", []string{"u", "--json", "--timeout", "5s"}, []string{"--json", "--timeout", "5s", "u"}},
		{"double dash terminator is positional", []string{"u", "--", "--json"}, []string{"u", "--json"}},
		{"lone dash is positional", []string{"-", "u"}, []string{"-", "u"}},
		{"unknown flag does not consume value", []string{"u", "--nope", "v"}, []string{"--nope", "u", "v"}},
		{"short help flag", []string{"u", "-h"}, []string{"-h", "u"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := reorderFlags(newFS(), c.in)
			if !slices.Equal(got, c.want) {
				t.Errorf("reorderFlags(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestExitCodeFor(t *testing.T) {
	t.Parallel()
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
		t.Run(string(c.code), func(t *testing.T) {
			t.Parallel()
			got := exitCodeFor(checker.Result{ReasonCode: string(c.code)})
			if got != c.want {
				t.Errorf("exitCodeFor(%q) = %d, want %d", c.code, got, c.want)
			}
		})
	}
}

func TestExitLabel(t *testing.T) {
	t.Parallel()
	cases := map[int]string{
		exitOK:       "OK",
		exitWarning:  "WARNING",
		exitCritical: "CRITICAL",
		exitUnknown:  "UNKNOWN",
		99:           "UNKNOWN",
	}
	for code, want := range cases {
		if got := exitLabel(code); got != want {
			t.Errorf("exitLabel(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestPrintHuman_IncludesAllDetailLines(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	res := checker.Result{
		URL:            "http://example.com",
		ValidFormat:    true,
		SiteUp:         true,
		PageExists:     true,
		StatusCode:     http.StatusOK,
		ResponseTimeMs: 42,
		Reason:         "ok",
		ReasonCode:     string(checker.ReasonOK),
		RedirectedTo:   "http://example.com/",
	}

	printHuman(&buf, res, exitOK)

	out := buf.String()
	for _, want := range []string{
		"OK",
		"http://example.com",
		"status_code:",
		"200",
		"response_time:",
		"42ms",
		"reason:",
		"ok (ok)",
		"redirected_to:",
		"http://example.com/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintHuman_OmitsEmptyOptionalLines(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	res := checker.Result{
		URL:        "http://example.com/missing",
		SiteUp:     true,
		PageExists: false,
		Reason:     "page not found",
		ReasonCode: string(checker.ReasonNotFound),
	}

	printHuman(&buf, res, exitWarning)

	out := buf.String()
	if !strings.Contains(out, "WARNING") {
		t.Errorf("output missing WARNING label:\n%s", out)
	}
	if strings.Contains(out, "status_code:") {
		t.Errorf("status_code line should be omitted when StatusCode is 0:\n%s", out)
	}
	if strings.Contains(out, "redirected_to:") {
		t.Errorf("redirected_to line should be omitted when empty:\n%s", out)
	}
}

// captureRun runs Run while capturing the process stdout/stderr. It must
// not be used from parallel tests because it swaps global file handles.
func captureRun(t *testing.T, args []string) (stdout, stderr string, code int) {
	t.Helper()

	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() { b, _ := io.ReadAll(rOut); outCh <- string(b) }()
	go func() { b, _ := io.ReadAll(rErr); errCh <- string(b) }()

	code = Run(args)

	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = origOut, origErr

	return <-outCh, <-errCh, code
}

func TestRun_Dispatcher(t *testing.T) {
	t.Run("version flag", func(t *testing.T) {
		out, _, code := captureRun(t, []string{"--version"})
		if code != exitOK {
			t.Fatalf("exit code = %d, want %d", code, exitOK)
		}
		if strings.TrimSpace(out) != version {
			t.Fatalf("output = %q, want version %q", out, version)
		}
	})

	t.Run("version command alias", func(t *testing.T) {
		for _, arg := range []string{"version", "-v"} {
			out, _, code := captureRun(t, []string{arg})
			if code != exitOK {
				t.Fatalf("%q: exit code = %d, want %d", arg, code, exitOK)
			}
			if strings.TrimSpace(out) != version {
				t.Fatalf("%q: output = %q, want version %q", arg, out, version)
			}
		}
	})

	t.Run("help flag", func(t *testing.T) {
		out, _, code := captureRun(t, []string{"--help"})
		if code != exitOK {
			t.Fatalf("exit code = %d, want %d", code, exitOK)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("output missing usage text: %q", out)
		}
	})

	t.Run("help command alias", func(t *testing.T) {
		out, _, code := captureRun(t, []string{"help"})
		if code != exitOK {
			t.Fatalf("exit code = %d, want %d", code, exitOK)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("output missing usage text: %q", out)
		}
	})

	t.Run("no args prints usage to stderr", func(t *testing.T) {
		out, errOut, code := captureRun(t, nil)
		if code != exitUnknown {
			t.Fatalf("exit code = %d, want %d", code, exitUnknown)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if !strings.Contains(errOut, "Usage:") {
			t.Fatalf("stderr missing usage text: %q", errOut)
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		_, errOut, code := captureRun(t, []string{"bogus"})
		if code != exitUnknown {
			t.Fatalf("exit code = %d, want %d", code, exitUnknown)
		}
		if !strings.Contains(errOut, "unknown command") {
			t.Fatalf("stderr missing unknown-command message: %q", errOut)
		}
	})
}

func TestRunServe_InvalidPortFlag(t *testing.T) {
	t.Parallel()
	var errOut bytes.Buffer
	code := runServe([]string{"--port", "not-an-int"}, &errOut)
	if code != exitUnknown {
		t.Fatalf("exit code = %d, want %d", code, exitUnknown)
	}
	if errOut.Len() == 0 {
		t.Fatal("expected an error message on stderr")
	}
}

// A negative port fails to bind immediately, which exercises the
// server-start error path without leaving a listener running.
func TestRunServe_BadPortIsCritical(t *testing.T) {
	t.Parallel()
	var errOut bytes.Buffer
	code := runServe([]string{"--port", "-1"}, &errOut)
	if code != exitCritical {
		t.Fatalf("exit code = %d, want %d; stderr=%s", code, exitCritical, errOut.String())
	}
	if errOut.Len() == 0 {
		t.Fatal("expected an error message on stderr")
	}
}
