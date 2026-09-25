// Command howdy is the command line for the Howdy web framework.
package main

import (
	"os"

	"howdy/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
