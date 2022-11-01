package util

import "fmt"

const (
	BANK              = "bank"
	AUTH              = "auth"
	DefaultIDTemplate = "*"
)

func GetIdentifierTemplatePerModule(module string, identifier string) string {
	return fmt.Sprintf("%s/%s", module, identifier)
}
