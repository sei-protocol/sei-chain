package types

import (
	fmt "fmt"
)

type AccessOperation struct {
	MessageKey         string
	AccessType         AccessType
	ResourceType       ResourceType
	ResourceIDTemplate string
}

func (a *AccessOperation) GetResourceIDTemplate(args []any) string {
	return fmt.Sprintf(a.ResourceIDTemplate, args...)
}
