package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	tmjson "github.com/sei-protocol/sei-chain/sei-tendermint/libs/json"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// GenValidatorCmd allows the generation of a keypair for a
// validator.
func MakeGenValidatorCommand() *cobra.Command {
	var keyType string
	cmd := &cobra.Command{
		Use:   "gen-validator",
		Short: "Generate new validator keypair",
		RunE: func(cmd *cobra.Command, args []string) error {
			pv, err := privval.GenFilePV("", "", keyType)
			if err != nil {
				return err
			}

			jsbz, err := tmjson.Marshal(pv)
			if err != nil {
				return fmt.Errorf("validator -> json: %w", err)
			}

			fmt.Printf("%v\n", string(jsbz))

			return nil
		},
	}

	cmd.Flags().StringVar(&keyType, "key", types.ABCIPubKeyTypeEd25519,
		"Key type to generate privval file with (ed25519 only)")

	return cmd
}
