// Command jc adalah alias pendek untuk jenderal (PRD 9).
package main

import (
	"os"

	"github.com/jenderalcode/jenderal/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
