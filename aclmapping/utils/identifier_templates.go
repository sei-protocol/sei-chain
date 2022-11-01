package util

import "fmt"

const (
	ACCOUNT           = "acc"
	BANK              = "bank"
	AUTH              = "auth"
	DefaultIDTemplate = "*"
)

func GetIdentifierTemplatePerModule(module string, identifier string) string {
	return fmt.Sprintf("%s/%s", module, identifier)
}

func GetPrefixedIdentifierTemplatePerModule(module string, identifier string, prefix string) string {
	return fmt.Sprintf("%s/%s/%s", module, prefix, identifier)
}
