package util

import "fmt"

const (
	BANK = "bank"
	AUTH = "auth"
	DEFAULT_ID_TEMPLATE = "*"
)

func GetIdentifierTemplatePerModule(module string, identifier string) string {
	return fmt.Sprintf("%s/%s", module, identifier)
}
