//go:build !linux || baremetal || polkawasm

package os

import "errors"

func Executable() (string, error) {
	return "", errors.New("Executable not implemented")
}
