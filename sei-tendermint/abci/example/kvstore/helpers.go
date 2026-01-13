package kvstore

import (
	"context"
	mrand "math/rand"

	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

// RandVals returns a list of cnt validators for initializing
// the application. Note that the keys are deterministically
// derived from the index in the array, while the power is
// random (Change this if not desired)
func RandVals(cnt int) []types.ValidatorUpdate {
	res := make([]types.ValidatorUpdate, cnt)
	for i := range res {
		// Random value between [0, 2^16 - 1]
		power := mrand.Uint32() & (1<<16 - 1) // nolint:gosec // G404: Use of weak random number generator
		keyBytes := tmrand.Bytes(len(crypto.PubKey{}.Bytes()))
		pubKey, err := ed25519.PublicKeyFromBytes(keyBytes)
		if err != nil {
			panic(err)
		}
		res[i] = types.ValidatorUpdate{
			PubKey: crypto.PubKeyToProto(pubKey),
			Power:  int64(power),
		}
	}
	return res
}

// InitKVStore initializes the kvstore app with some data,
// which allows tests to pass and is fine as long as you
// don't make any tx that modify the validator state
func InitKVStore(ctx context.Context, app *PersistentKVStoreApplication) error {
	_, err := app.InitChain(ctx, &types.RequestInitChain{
		Validators: RandVals(1),
	})
	return err
}
