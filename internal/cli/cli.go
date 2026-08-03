// Package cli wires command-line argument parsing to uplet's subcommands.
package cli

import (
	"fmt"
	"os"
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

// Run dispatches args to a subcommand and returns the process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 3
	}

	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(os.Stdout, usage)
		return 0
	case "--version", "-v", "version":
		fmt.Fprintln(os.Stdout, version)
		return 0
	case "check":
		fmt.Fprintln(os.Stderr, "check: not implemented yet")
		return 3
	case "serve":
		fmt.Fprintln(os.Stderr, "serve: not implemented yet")
		return 3
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 3
	}
}
