package main

import (
	"os"
	"time"

	"github.com/chrismo/plimsoll/cli"
)

func main() {
	y, m, d := time.Now().Date()
	app := &cli.App{Stdout: os.Stdout, Stderr: os.Stderr, Today: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
	os.Exit(app.Run(os.Args[1:]))
}
