package cli

import (
	flag "github.com/spf13/pflag"
)

const (
	// The connection end identifier on the controller chain
	FlagConnectionID = "connection-id"
)

// common flagsets to add to various functions
var (
	fsConnectionID = flag.NewFlagSet("", flag.ContinueOnError)
)

func init() {
	fsConnectionID.String(FlagConnectionID, "", "Connection ID")
}
