//go:build linux && !android && guiacceptance

// This opt-in executable changes the configured user's live VPN connections.
package main

import (
	"fmt"
	"os"
	"quiclab/internal/desktopui"
)

func main() {
	if err := desktopui.RunLiveAcceptance(); err != nil {
		fmt.Fprintln(os.Stderr, "GUI_ACCEPT failed:", err)
		os.Exit(1)
	}
}
