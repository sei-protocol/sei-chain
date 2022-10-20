package keeper_test

import (
	"encoding/hex"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	nitrokeeper "github.com/sei-protocol/sei-chain/x/nitro/keeper"
	"github.com/sei-protocol/sei-chain/x/nitro/types"
	"github.com/stretchr/testify/require"
)

func TestRecordTransactionData(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	server := nitrokeeper.NewMsgServerImpl(*keeper)
	// set with non-whitelisted addr
	_, err := server.RecordTransactionData(sdk.WrapSDKContext(ctx), &types.MsgRecordTransactionData{
		Sender:    "someone",
		Slot:      1,
		StateRoot: "1234",
		Txs:       []string{"5678"},
	})
	require.NotNil(t, err)
	// set with whitelisted addr
	keeper.SetParams(ctx, types.Params{WhitelistedTxSenders: []string{"sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"}})
	_, err = server.RecordTransactionData(sdk.WrapSDKContext(ctx), &types.MsgRecordTransactionData{
		Sender:    "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Slot:      1,
		StateRoot: "1234",
		Txs:       []string{"5678"},
	})
	require.Nil(t, err)
	sender, exists := keeper.GetSender(ctx, 1)
	require.True(t, exists)
	require.Equal(t, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", sender)
	root, err := keeper.GetStateRoot(ctx, 1)
	require.Nil(t, err)
	require.Equal(t, "1234", hex.EncodeToString(root))
	txs, err := keeper.GetTransactionData(ctx, 1)
	require.Nil(t, err)
	require.Equal(t, "5678", hex.EncodeToString(txs[0]))
	// set with invalid root
	_, err = server.RecordTransactionData(sdk.WrapSDKContext(ctx), &types.MsgRecordTransactionData{
		Sender:    "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Slot:      1,
		StateRoot: "123",
		Txs:       []string{"5678"},
	})
	require.NotNil(t, err)
	// set with invalid tx
	_, err = server.RecordTransactionData(sdk.WrapSDKContext(ctx), &types.MsgRecordTransactionData{
		Sender:    "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Slot:      1,
		StateRoot: "1234",
		Txs:       []string{"567"},
	})
	require.NotNil(t, err)
	// set for existing slot
	_, err = server.RecordTransactionData(sdk.WrapSDKContext(ctx), &types.MsgRecordTransactionData{
		Sender:    "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Slot:      1,
		StateRoot: "1234",
		Txs:       []string{"6789"},
	})
	require.NotNil(t, err)
}

func TestSubmitFraudChallenge(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	server := nitrokeeper.NewMsgServerImpl(*keeper)
	stateRoot, proof := createMockMerkleProof()
	// set state root with mock merkle root
	keeper.SetParams(ctx, types.Params{WhitelistedTxSenders: []string{"sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"}})
	_, err := server.RecordTransactionData(sdk.WrapSDKContext(ctx), &types.MsgRecordTransactionData{
		Sender:    "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Slot:      1,
		StateRoot: hex.EncodeToString(stateRoot),
		Txs:       []string{"5678"},
	})
	require.Nil(t, err)

	// end slot doesn't exist
	_, err = server.SubmitFraudChallenge(sdk.WrapSDKContext(ctx), &types.MsgSubmitFraudChallenge{
		StartSlot:   0,
		EndSlot:     2,
		FraudState:  "123",
		MerkleProof: proof,
	})
	require.NotNil(t, err)

	// invalid merkle proof
	proof.Hash[0] = "efg"
	_, err = server.SubmitFraudChallenge(sdk.WrapSDKContext(ctx), &types.MsgSubmitFraudChallenge{
		StartSlot:   0,
		EndSlot:     1,
		FraudState:  "123",
		MerkleProof: proof,
	})
	require.NotNil(t, err)

	// invalid original state root
	_, proof = createMockMerkleProof()
	proof.Commitment = "efg"
	_, err = server.SubmitFraudChallenge(sdk.WrapSDKContext(ctx), &types.MsgSubmitFraudChallenge{
		StartSlot:   0,
		EndSlot:     1,
		FraudState:  "123",
		MerkleProof: proof,
	})
	require.NotNil(t, err)

	// happy path
	_, proof = createMockMerkleProof()
	_, err = server.SubmitFraudChallenge(sdk.WrapSDKContext(ctx), &types.MsgSubmitFraudChallenge{
		StartSlot:   0,
		EndSlot:     1,
		FraudState:  "123",
		MerkleProof: proof,
	})
	require.Nil(t, err)
}
