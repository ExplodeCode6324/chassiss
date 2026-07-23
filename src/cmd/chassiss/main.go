package main

import (
	"os"

	"github.com/ExplodeCode6324/chassiss/internal/app"
)

func main() {
	os.Exit(app.RunWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
