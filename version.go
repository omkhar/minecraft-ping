package main

import "fmt"

var cliVersion = "dev"

func versionLine() string {
	return fmt.Sprintf("minecraft-ping %s", cliVersion)
}
