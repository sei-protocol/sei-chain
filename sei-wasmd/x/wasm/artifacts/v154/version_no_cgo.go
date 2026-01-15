//go:build !cgo

package v154

import (
	"fmt"
)

func libwasmvmVersionImpl() (string, error) {
	return "", fmt.Errorf("libwasmvm unavailable since cgo is disabled")
}
