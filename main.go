package main

import (
	"os"

	"github.com/mona-actions/gh-repo-map/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
