package main

import (
	"runtime"

	"autohide/cmd"
)

var version = "dev"

func init() {
	runtime.LockOSThread()
}

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}
