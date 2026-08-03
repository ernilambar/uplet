package main

import (
	"os"

	"github.com/ernilambar/uplet/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
