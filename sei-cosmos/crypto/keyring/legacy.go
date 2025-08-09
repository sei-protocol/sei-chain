package keyring

import (
	"fmt"
)

func infoKey(name string) string   { return fmt.Sprintf("%s.%s", name, infoSuffix) }
func infoKeyBz(name string) []byte { return []byte(infoKey(name)) }

// KeybaseOption overrides options for the db.
type KeybaseOption func(*kbOptions)

type kbOptions struct {
}
