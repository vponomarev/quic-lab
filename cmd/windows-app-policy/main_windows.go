package main

import (
	"encoding/json"
	"fmt"
	"os"
	"quiclab/internal/approuting"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-running" {
		apps, skipped, e := approuting.RunningApplications()
		if e != nil {
			fmt.Fprintln(os.Stderr, "application inventory unavailable")
			os.Exit(1)
		}
		if json.NewEncoder(os.Stdout).Encode(struct {
			Applications []approuting.RunningApplication `json:"applications"`
			Unavailable  int                             `json:"unavailable"`
		}{apps, skipped}) != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: windows-app-policy <absolute.exe> [...]; read-only identity inspection")
		os.Exit(2)
	}
	ids := make([]approuting.Identity, 0, len(os.Args)-1)
	for _, path := range os.Args[1:] {
		id, err := approuting.ResolveExecutable(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "executable identity unavailable")
			os.Exit(1)
		}
		ids = append(ids, id)
	}
	if json.NewEncoder(os.Stdout).Encode(ids) != nil {
		os.Exit(1)
	}
}
