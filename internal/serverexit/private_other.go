//go:build !linux

package serverexit

import "net"

func privateDirectory(string) error { return ErrConfig }
func privateListener(string,string)(net.Listener,func(),error){return nil,nil,ErrConfig}
