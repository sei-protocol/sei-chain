package docs

// imported embed function
import "embed"

//go:embed static
var Docs embed.FS
