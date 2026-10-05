// Command jenderal adalah entry point utama CLI + TUI + server.
package main

import (
	"os"

	"github.com/jenderalcode/jenderal/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
