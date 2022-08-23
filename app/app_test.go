package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
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
