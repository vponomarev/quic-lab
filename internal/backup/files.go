package backup

import "os"

func Replace(oldPath, newPath string) error {
	return Publish(newPath, func() error { return os.Rename(oldPath, newPath) })
}
func Remove(path string) error     { return Publish(path, func() error { return os.Remove(path) }) }
func RemoveTree(path string) error { return Publish(path, func() error { return os.RemoveAll(path) }) }
