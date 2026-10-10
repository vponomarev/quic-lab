//go:build !linux

package backup

import (
	"context"
	"errors"
	"os"
)

func EnablePublication(string) error { return errors.New("server backups require Linux") }
func FreezePublication(context.Context, string) (func(), error) {
	return nil, errors.New("server backups require Linux")
}
func PublicationEpoch(string) (string, error)       { return "", errors.New("server backups require Linux") }
func Publish(_ string, mutation func() error) error { return mutation() }

func AcquireProcessLock(string, string) (*os.File, error) {
	return nil, errors.New("server backups require Linux")
}

func copyOwnership(string, string) error { return errors.New("server restore requires Linux") }
