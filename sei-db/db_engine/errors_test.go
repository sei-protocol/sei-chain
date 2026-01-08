package db_engine

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	// Direct check
	if !IsNotFound(ErrNotFound) {
		t.Fatal("IsNotFound should return true for ErrNotFound")
	}

	// Wrapped error
	wrapped := fmt.Errorf("wrapper: %w", ErrNotFound)
	if !IsNotFound(wrapped) {
		t.Fatal("IsNotFound should return true for wrapped ErrNotFound")
	}

	// Unrelated error
	other := errors.New("some other error")
	if IsNotFound(other) {
		t.Fatal("IsNotFound should return false for unrelated errors")
	}

	// Nil error
	if IsNotFound(nil) {
		t.Fatal("IsNotFound should return false for nil")
	}
}
