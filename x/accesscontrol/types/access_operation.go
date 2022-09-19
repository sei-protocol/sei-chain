package types

import (
	fmt "fmt"
)

type AcessOperation struct {
	MessageKey         string
	AccessType         AccessType
	ResourceType       ResourceType
	ResourceIDTemplate string
}

func (a *AcessOperation) GetResourceIDTemplate(args []any) string {
	return fmt.Sprintf(a.ResourceIDTemplate, args...)
}
