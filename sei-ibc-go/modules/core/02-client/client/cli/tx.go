package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/exported"
)

// NewCreateClientCmd defines the command to create a new IBC light client.
func NewCreateClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [path/to/client_state.json] [path/to/consensus_state.json]",
		Short: "create new IBC client",
		Long: `create a new IBC client with the specified client state and consensus state
	- ClientState JSON example: {"@type":"/ibc.lightclients.solomachine.v1.ClientState","sequence":"1","frozen_sequence":"0","consensus_state":{"public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AtK50+5pJOoaa04qqAqrnyAqsYrwrR/INnA6UPIaYZlp"},"diversifier":"testing","timestamp":"10"},"allow_update_after_proposal":false}
	- ConsensusState JSON example: {"@type":"/ibc.lightclients.solomachine.v1.ConsensusState","public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AtK50+5pJOoaa04qqAqrnyAqsYrwrR/INnA6UPIaYZlp"},"diversifier":"testing","timestamp":"10"}`,
		Example: fmt.Sprintf("%s tx ibc %s create [path/to/client_state.json] [path/to/consensus_state.json] --from node0 --home ../node0/<app>cli --chain-id $CID", version.AppName, types.SubModuleName),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			cdc := codec.NewProtoCodec(clientCtx.InterfaceRegistry)

			// attempt to unmarshal client state argument
			var clientState exported.ClientState
			clientContentOrFileName := args[0]
			if err := cdc.UnmarshalInterfaceJSON([]byte(clientContentOrFileName), &clientState); err != nil {

				// check for file path if JSON input is not provided
				contents, err := os.ReadFile(filepath.Clean(clientContentOrFileName))
				if err != nil {
					return fmt.Errorf("neither JSON input nor path to .json file for client state were provided: %w", err)
				}

				if err := cdc.UnmarshalInterfaceJSON(contents, &clientState); err != nil {
					return fmt.Errorf("error unmarshalling client state file: %w", err)
				}
			}

			// attempt to unmarshal consensus state argument
			var consensusState exported.ConsensusState
			consensusContentOrFileName := args[1]
			if err := cdc.UnmarshalInterfaceJSON([]byte(consensusContentOrFileName), &consensusState); err != nil {

				// check for file path if JSON input is not provided
				contents, err := os.ReadFile(filepath.Clean(consensusContentOrFileName))
				if err != nil {
					return fmt.Errorf("neither JSON input nor path to .json file for consensus state were provided: %w", err)
				}

				if err := cdc.UnmarshalInterfaceJSON(contents, &consensusState); err != nil {
					return fmt.Errorf("error unmarshalling consensus state file: %w", err)
				}
			}

			msg, err := types.NewMsgCreateClient(clientState, consensusState, clientCtx.GetFromAddress().String())
			if err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewUpdateClientCmd defines the command to update an IBC client.
func NewUpdateClientCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "update [client-id] [path/to/header.json]",
		Short:   "update existing client with a header",
		Long:    "update existing client with a header",
		Example: fmt.Sprintf("%s tx ibc %s update [client-id] [path/to/header.json] --from node0 --home ../node0/<app>cli --chain-id $CID", version.AppName, types.SubModuleName),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			clientID := args[0]

			cdc := codec.NewProtoCodec(clientCtx.InterfaceRegistry)

			var header exported.Header
			headerContentOrFileName := args[1]
			if err := cdc.UnmarshalInterfaceJSON([]byte(headerContentOrFileName), &header); err != nil {

				// check for file path if JSON input is not provided
				contents, err := os.ReadFile(filepath.Clean(headerContentOrFileName))
				if err != nil {
					return fmt.Errorf("neither JSON input nor path to .json file for header were provided: %w", err)
				}

				if err := cdc.UnmarshalInterfaceJSON(contents, &header); err != nil {
					return fmt.Errorf("error unmarshalling header file: %w", err)
				}
			}

			msg, err := types.NewMsgUpdateClient(clientID, header, clientCtx.GetFromAddress().String())
			if err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}
}

// NewSubmitMisbehaviourCmd defines the command to submit a misbehaviour to prevent
// future updates.
func NewSubmitMisbehaviourCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "misbehaviour [path/to/misbehaviour.json]",
		Short:   "submit a client misbehaviour",
		Long:    "submit a client misbehaviour to prevent future updates",
		Example: fmt.Sprintf("%s tx ibc %s misbehaviour [path/to/misbehaviour.json] --from node0 --home ../node0/<app>cli --chain-id $CID", version.AppName, types.SubModuleName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			cdc := codec.NewProtoCodec(clientCtx.InterfaceRegistry)

			var misbehaviour exported.Misbehaviour
			misbehaviourContentOrFileName := args[0]
			if err := cdc.UnmarshalInterfaceJSON([]byte(misbehaviourContentOrFileName), &misbehaviour); err != nil {

				// check for file path if JSON input is not provided
				contents, err := os.ReadFile(filepath.Clean(misbehaviourContentOrFileName))
				if err != nil {
					return fmt.Errorf("neither JSON input nor path to .json file for misbehaviour were provided: %w", err)
				}

				if err := cdc.UnmarshalInterfaceJSON(contents, misbehaviour); err != nil {
					return fmt.Errorf("error unmarshalling misbehaviour file: %w", err)
				}
			}

			msg, err := types.NewMsgSubmitMisbehaviour(misbehaviour.GetClientID(), misbehaviour, clientCtx.GetFromAddress().String())
			if err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}
}

// NewUpgradeClientCmd defines the command to upgrade an IBC light client.
func NewUpgradeClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade [client-identifier] [path/to/client_state.json] [path/to/consensus_state.json] [upgrade-client-proof] [upgrade-consensus-state-proof]",
		Short: "upgrade an IBC client",
		Long: `upgrade the IBC client associated with the provided client identifier while providing proof committed by the counterparty chain to the new client and consensus states
	- ClientState JSON example: {"@type":"/ibc.lightclients.solomachine.v1.ClientState","sequence":"1","frozen_sequence":"0","consensus_state":{"public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AtK50+5pJOoaa04qqAqrnyAqsYrwrR/INnA6UPIaYZlp"},"diversifier":"testing","timestamp":"10"},"allow_update_after_proposal":false}
	- ConsensusState JSON example: {"@type":"/ibc.lightclients.solomachine.v1.ConsensusState","public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AtK50+5pJOoaa04qqAqrnyAqsYrwrR/INnA6UPIaYZlp"},"diversifier":"testing","timestamp":"10"}`,
		Example: fmt.Sprintf("%s tx ibc %s upgrade [client-identifier] [path/to/client_state.json] [path/to/consensus_state.json] [client-state-proof] [consensus-state-proof] --from node0 --home ../node0/<app>cli --chain-id $CID", version.AppName, types.SubModuleName),
		Args:    cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			cdc := codec.NewProtoCodec(clientCtx.InterfaceRegistry)
			clientID := args[0]

			// attempt to unmarshal client state argument
			var clientState exported.ClientState
			clientContentOrFileName := args[1]
			if err := cdc.UnmarshalInterfaceJSON([]byte(clientContentOrFileName), &clientState); err != nil {

				// check for file path if JSON input is not provided
				contents, err := os.ReadFile(filepath.Clean(clientContentOrFileName))
				if err != nil {
					return fmt.Errorf("neither JSON input nor path to .json file for client state were provided: %w", err)
				}

				if err := cdc.UnmarshalInterfaceJSON(contents, &clientState); err != nil {
					return fmt.Errorf("error unmarshalling client state file: %w", err)
				}
			}

			// attempt to unmarshal consensus state argument
			var consensusState exported.ConsensusState
			consensusContentOrFileName := args[2]
			if err := cdc.UnmarshalInterfaceJSON([]byte(consensusContentOrFileName), &consensusState); err != nil {

				// check for file path if JSON input is not provided
				contents, err := os.ReadFile(filepath.Clean(consensusContentOrFileName))
				if err != nil {
					return fmt.Errorf("neither JSON input nor path to .json file for consensus state were provided: %w", err)
				}

				if err := cdc.UnmarshalInterfaceJSON(contents, &consensusState); err != nil {
					return fmt.Errorf("error unmarshalling consensus state file: %w", err)
				}
			}

			proofUpgradeClient := []byte(args[3])
			proofUpgradeConsensus := []byte(args[4])

			msg, err := types.NewMsgUpgradeClient(clientID, clientState, consensusState, proofUpgradeClient, proofUpgradeConsensus, clientCtx.GetFromAddress().String())
			if err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
