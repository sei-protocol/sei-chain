package types

import "fmt"

type (
	// ErrInvalidCommitHeight is returned when we encounter a commit with an
	// unexpected height.
	ErrInvalidCommitHeight struct {
		Expected int64
		Actual   int64
	}

	// ErrInvalidCommitSignatures is returned when we encounter a commit where
	// the number of signatures doesn't match the number of validators.
	ErrInvalidCommitSignatures struct {
		Expected int
		Actual   int
	}
)
type errBadBlockID struct{ error }
type errBadSig struct{ error }

func NewErrInvalidCommitHeight(expected, actual int64) ErrInvalidCommitHeight {
	return ErrInvalidCommitHeight{
		Expected: expected,
		Actual:   actual,
	}
}

func (e ErrInvalidCommitHeight) Error() string {
	return fmt.Sprintf("Invalid commit -- wrong height: want %v, got %v", e.Expected, e.Actual)
}

func NewErrInvalidCommitSignatures(expected, actual int) ErrInvalidCommitSignatures {
	return ErrInvalidCommitSignatures{
		Expected: expected,
		Actual:   actual,
	}
}

func (e ErrInvalidCommitSignatures) Error() string {
	return fmt.Sprintf("Invalid commit -- wrong set size: want %v, got %v", e.Expected, e.Actual)
}
