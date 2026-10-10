//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
	"quiclab/internal/admin"
)

func runBackup(_ context.Context, _ []string, _ io.Reader, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "server backup CLI requires Linux")
	return 2
}

func initBackupStorage(string) (func(), error) { return func() {}, nil }
func startServerBackups(context.Context, *admin.Web, serverConfig) (func(), error) {
	return func() {}, nil
}
