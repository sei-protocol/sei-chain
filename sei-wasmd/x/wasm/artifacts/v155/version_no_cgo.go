//go:build !cgo

package v155

import (
	"fmt"
)

func libwasmvmVersionImpl() (string, error) {
	return "", fmt.Errorf("libwasmvm unavailable since cgo is disabled")
}
