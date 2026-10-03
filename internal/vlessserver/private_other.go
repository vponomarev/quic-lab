//go:build !linux

package vlessserver

import "net"

func privateDirectory(string) error { return errConfig }

func privateListener(string, string) (net.Listener, func(), error) { return nil, nil, errConfig }
