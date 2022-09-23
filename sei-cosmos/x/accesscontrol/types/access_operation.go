package types

import (
	fmt "fmt"
)

func (a *AccessOperation) GetResourceIDTemplate(args []any) string {
	return fmt.Sprintf(a.GetIdentifierTemplate(), args...)
}
