// Command jenderalcode adalah entry point utama CLI + TUI + server.
package main

import (
	"os"

	"github.com/mohammadirham37/jenderal_code/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
