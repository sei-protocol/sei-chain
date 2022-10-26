package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/k0kubun/pp/v3"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestEmptyBlockIdempotency(t *testing.T) {
	commitData := [][]byte{}
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	for i := 1; i <= 10; i++ {
		testWrapper := app.NewTestWrapper(t, tm, valPub)
		res, _ := testWrapper.App.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
		testWrapper.App.Commit(context.Background())
		data := res.AppHash
		commitData = append(commitData, data)
	}

	referenceData := commitData[0]
	for _, data := range commitData[1:] {
		require.Equal(t, len(referenceData), len(data))
	}
}

func TestGetChannelsFromSignalMapping(t *testing.T) {
	dag := acltypes.NewDag()
	commit := *acltypes.CommitAccessOp()
	writeA := sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	readA := sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	readAll := sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_ANY,
		IdentifierTemplate: "*",
	}

	dag.AddNodeBuildDependency(0, 0, writeA)
	dag.AddNodeBuildDependency(0, 0, commit)
	dag.AddNodeBuildDependency(1, 0, readAll)
	dag.AddNodeBuildDependency(1, 0, commit)
	dag.AddNodeBuildDependency(2, 0, writeA)
	dag.AddNodeBuildDependency(2, 0, commit)
	dag.AddNodeBuildDependency(3, 0, writeA)
	dag.AddNodeBuildDependency(3, 0, commit)

	dag.AddNodeBuildDependency(0, 1, writeA)
	dag.AddNodeBuildDependency(0, 1, commit)
	dag.AddNodeBuildDependency(1, 1, readA)
	dag.AddNodeBuildDependency(1, 1, commit)

	completionSignalsMap, blockingSignalsMap := dag.CompletionSignalingMap, dag.BlockingSignalsMap

	pp.Default.SetColoringEnabled(false)

	resultCompletionSignalsMap := app.GetChannelsFromSignalMapping(completionSignalsMap[0])
	resultBlockingSignalsMap := app.GetChannelsFromSignalMapping(blockingSignalsMap[1])

	require.True(t, len(resultCompletionSignalsMap) > 1)
	require.True(t, len(resultBlockingSignalsMap) > 1)
}
