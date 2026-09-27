package main

import "os/exec"

func notify(message string) {
	_ = exec.Command("/usr/bin/osascript", "-e", "on run argv\ndisplay alert \"QUIC Lab Capture\" message (item 1 of argv) as critical\nend run", message).Run()
}
