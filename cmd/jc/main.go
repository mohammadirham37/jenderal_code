// Command jc adalah alias pendek untuk jenderalcode (PRD 9).
package main

import (
	"os"

	"github.com/mohammadirham37/jenderal_code/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
