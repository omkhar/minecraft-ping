package main

import (
	"io"
	"os"
)

var (
	mainArgs             = func() []string { return os.Args }
	mainStdout io.Writer = os.Stdout
	mainStderr io.Writer = os.Stderr
	mainExit             = os.Exit
)

func main() {
	mainExit(execute(mainArgs(), mainStdout, mainStderr, defaultPing))
}
