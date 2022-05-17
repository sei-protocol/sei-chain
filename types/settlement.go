package types

import "fmt"

type Settlement struct {
	To       string
	Quantity uint64
	Denom    string
}

func (s Settlement) String() string {
	return fmt.Sprintf("To %s of %d%s", s.To, s.Quantity, s.Denom)
}
