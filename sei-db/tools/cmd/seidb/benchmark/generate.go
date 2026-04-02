package benchmark

import (
	"fmt"

	"github.com/spf13/cobra"
)

func GenerateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Deprecated: generate command has been removed (sei-iavl dependency removed)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("The generate command has been deprecated and removed.")
		},
	}
}
