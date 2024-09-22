package evmrpc_test

import (
	// "fmt"
	"testing"
)

func TestGetSeiTxReceipt(t *testing.T) {
	testGetTxReceipt(t, "sei")
}

func TestSeiGetTransaction(t *testing.T) {
	testGetTransaction(t, "sei")
}
