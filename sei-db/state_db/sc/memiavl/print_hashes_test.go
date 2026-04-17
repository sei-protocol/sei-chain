package memiavl

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestPrintRefHashes(t *testing.T) {
	for i, h := range RefHashes {
		fmt.Printf("HASH[%d]=%s\n", i, hex.EncodeToString(h))
	}
}
