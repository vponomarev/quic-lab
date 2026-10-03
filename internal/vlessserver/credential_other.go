//go:build !linux

package vlessserver

import "os"

func systemdPrivateCredential(string, os.FileInfo) bool { return false }
