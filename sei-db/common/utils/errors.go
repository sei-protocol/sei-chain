package utils

import (
	"errors"
	"strings"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// StoreCodespace defines the store package's unique error code space.
const StoreCodespace = "store"

var (
	ErrKeyEmpty      = sdkerrors.Register(StoreCodespace, 1, "key empty")
	ErrStartAfterEnd = sdkerrors.Register(StoreCodespace, 2, "start key after end key")
)

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
// Join returns nil if errs contains no non-nil values.
// The error formats as the concatenation of the strings obtained
// by calling the Error method of each element of errs, with a newline
// between each string.
func Join(errs ...error) error {
	var errStrs []string
	numErrs := 0
	for _, err := range errs {
		if err != nil {
			numErrs++
			if err.Error() != "" {
				errStrs = append(errStrs, err.Error())
			}
		}
	}

	if numErrs <= 0 {
		return nil
	}

	return errors.New(strings.Join(errStrs, "\n"))

}
