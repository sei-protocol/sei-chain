package backends

import "fmt"

type ErrKeyNotFound struct {
	key string
}

func (e *ErrKeyNotFound) Error() string {
	return fmt.Sprintf("Key %s not found", e.key)
}
