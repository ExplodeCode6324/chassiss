package main

import (
	"os"

	"github.com/ExplodeCode6324/chassiss/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr))
}
