package types

import (
	"strings"

	"gopkg.in/yaml.v2"
)

// String implements fmt.Stringer interface
func (d Denom) String() string {
	out, _ := yaml.Marshal(d)
	return string(out)
}

// Equal implements equal interface
func (d Denom) Equal(d1 *Denom) bool {
	return d.Name == d1.Name
}

// DenomList is array of Denom
type DenomList []Denom

func (dl DenomList) Contains(denom string) bool {
	for _, d := range dl {
		if d.Name == denom {
			return true
		}
	}
	return false
}

// String implements fmt.Stringer interface
func (dl DenomList) String() (out string) {
	for _, d := range dl {
		out += d.String() + "\n"
	}
	return strings.TrimSpace(out)
}
