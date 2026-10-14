//go:build !windows

package awg

import "github.com/amnezia-vpn/amneziawg-go/conn"

func newPlatformBind() conn.Bind { return conn.NewStdNetBind() }
