package app_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
)

func TestEmptyBlockIdempotency(t *testing.T) {
	commitData := [][]byte{}
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	for i := 1; i <= 10; i++ {
		testWrapper := app.NewTestWrapper(t, tm, valPub)
		testWrapper.BeginBlock()
		testWrapper.EndBlock()
		data := testWrapper.App.Commit().Data
		commitData = append(commitData, data)
	}

	referenceData := commitData[0]
	for _, data := range commitData[1:] {
		require.Equal(t, len(referenceData), len(data))
	}
}
