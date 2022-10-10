package aclmapping

import "fmt"


const (
	BANK = "bank"
	AUTH = "auth"

)

func getIdentifierTemplatePerModule(module string, identifier string) string {
	return fmt.Sprintf("%s/%s", module, identifier)
}
