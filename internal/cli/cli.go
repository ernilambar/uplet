// Package cli wires command-line argument parsing to uplet's subcommands.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ernilambar/uplet/internal/checker"
	"github.com/ernilambar/uplet/internal/server"
)

// version is the build version, overridable at release time via
// -ldflags "-X github.com/ernilambar/uplet/internal/cli.version=vX.Y.Z".
var version = "dev"

const usage = `uplet — URL uptime and validity checker.

Usage:
  uplet check <url> [--json] [--timeout <dur>]   Check a single URL
  uplet serve [--port <port>]                     Run the local HTTP server
  uplet --help                                    Show this help
  uplet --version                                 Show version

Exit codes (check):
  0  OK          site up, page exists
  1  WARNING     site up, page missing
  2  CRITICAL    definitively down / unregistered / blocked
  3  UNKNOWN     bad format or indeterminate (timeout)
`

const (
	exitOK       = 0
	exitWarning  = 1
	exitCritical = 2
	exitUnknown  = 3
)

// Run dispatches args to a subcommand and returns the process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return exitUnknown
	}

	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(os.Stdout, usage)
		return exitOK
	case "--version", "-v", "version":
		fmt.Fprintln(os.Stdout, version)
		return exitOK
	case "check":
		return runCheck(args[1:], os.Stdout, os.Stderr)
	case "serve":
		return runServe(args[1:], os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return exitUnknown
	}
}

// reorderFlags moves flag tokens ahead of positional arguments, since
// flag.FlagSet.Parse stops parsing flags at the first non-flag argument and
// would otherwise reject "check <url> --json" (flags after the URL).
func reorderFlags(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if arg == "-" || len(arg) == 0 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}

		flags = append(flags, arg)
		if strings.Contains(arg, "=") {
			continue
		}

		name := strings.TrimLeft(arg, "-")
		if name == "h" || name == "help" {
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		if bv, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bv.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "output the result as JSON")
	timeout := fs.Duration("timeout", 10*time.Second, "per-check timeout")

	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return exitUnknown
	}

	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintf(stderr, "check: expected exactly one URL\n\n%s", usage)
		return exitUnknown
	}
	rawURL := rest[0]

	c := checker.New()
	c.Timeout = *timeout
	res := c.Check(context.Background(), rawURL)
	code := exitCodeFor(res)

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		printHuman(stdout, res, code)
	}

	return code
}

func runServe(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", server.DefaultPort, "port to listen on (binds to 127.0.0.1 only)")

	if err := fs.Parse(args); err != nil {
		return exitUnknown
	}

	srv := server.New()
	srv.Port = *port
	srv.Logger = log.New(stderr, "", log.LstdFlags)

	if err := srv.Run(context.Background()); err != nil {
		fmt.Fprintf(stderr, "serve: %v\n", err)
		return exitCritical
	}
	return exitOK
}

func exitCodeFor(res checker.Result) int {
	switch res.ReasonCode {
	case string(checker.ReasonOK), string(checker.ReasonTooLarge):
		return exitOK
	case string(checker.ReasonNotFound), string(checker.ReasonRedirectNotFound), string(checker.ReasonClientError):
		return exitWarning
	case string(checker.ReasonInvalidFormat), string(checker.ReasonTimeout):
		return exitUnknown
	default: // network_error, dns_error, tls_error, blocked_target, server_error
		return exitCritical
	}
}

func exitLabel(code int) string {
	switch code {
	case exitOK:
		return "OK"
	case exitWarning:
		return "WARNING"
	case exitCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

func printHuman(w io.Writer, res checker.Result, code int) {
	fmt.Fprintf(w, "%s  %s\n", exitLabel(code), res.URL)
	if res.StatusCode != 0 {
		fmt.Fprintf(w, "  status_code:    %d\n", res.StatusCode)
	}
	fmt.Fprintf(w, "  response_time:  %dms\n", res.ResponseTimeMs)
	fmt.Fprintf(w, "  reason:         %s (%s)\n", res.Reason, res.ReasonCode)
	if res.RedirectedTo != "" {
		fmt.Fprintf(w, "  redirected_to:  %s\n", res.RedirectedTo)
	}
}
