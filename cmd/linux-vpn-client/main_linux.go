//go:build linux && !android

package main

import (
	"os"
	"quiclab/internal/linuxcli"
)

func main() { os.Exit(linuxcli.Run(os.Args[1:])) }
