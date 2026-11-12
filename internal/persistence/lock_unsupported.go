//go:build !darwin && !linux

package persistence

import "os"

func lockDataDirectory(string) (*os.File, error) {
	return nil, ErrUnsupportedPlatform
}
