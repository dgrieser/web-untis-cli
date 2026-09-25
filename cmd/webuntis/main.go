// Command webuntis is a read-only WebUntis command line client.
package main

import (
	"os"

	"github.com/dgrieser/web-untis-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
