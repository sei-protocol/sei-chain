package testsuite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	mrand "math/rand"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
)

func InitChain(ctx context.Context, client types.Application) error {
	vals := make([]types.ValidatorUpdate, 10)
	for i := range vals {
		keyBytes := tmrand.Bytes(len(crypto.PubKey{}.Bytes()))
		pubkey, err := ed25519.PublicKeyFromBytes(keyBytes)
		if err != nil {
			return err
		}
		// nolint:gosec // G404: Use of weak random number generator
		power := mrand.Int()
		vals[i] = types.ValidatorUpdate{
			PubKey: crypto.PubKeyToProto(pubkey),
			Power:  int64(power),
		}
	}
	_, err := client.InitChain(ctx, &types.RequestInitChain{
		Validators: vals,
	})
	if err != nil {
		fmt.Printf("Failed test: InitChain - %v\n", err)
		return err
	}
	fmt.Println("Passed test: InitChain")
	return nil
}

func Commit(ctx context.Context, client types.Application) error {
	_, err := client.Commit(ctx)
	if err != nil {
		fmt.Println("Failed test: Commit")
		fmt.Printf("error while committing: %v\n", err)
		return err
	}
	fmt.Println("Passed test: Commit")
	return nil
}

func FinalizeBlock(ctx context.Context, client types.Application, txBytes [][]byte, codeExp []uint32, dataExp []byte, hashExp []byte) error {
	res, _ := client.FinalizeBlock(ctx, &types.RequestFinalizeBlock{Txs: txBytes})
	appHash := res.AppHash
	for i, tx := range res.TxResults {
		code, data, log := tx.Code, tx.Data, tx.Log
		if code != codeExp[i] {
			fmt.Println("Failed test: FinalizeBlock")
			fmt.Printf("FinalizeBlock response code was unexpected. Got %v expected %v. Log: %v\n",
				code, codeExp, log)
			return errors.New("FinalizeBlock error")
		}
		if !bytes.Equal(data, dataExp) {
			fmt.Println("Failed test:  FinalizeBlock")
			fmt.Printf("FinalizeBlock response data was unexpected. Got %X expected %X\n",
				data, dataExp)
			return errors.New("FinalizeBlock  error")
		}
	}
	if !bytes.Equal(appHash, hashExp) {
		fmt.Println("Failed test: FinalizeBlock")
		fmt.Printf("Application hash was unexpected. Got %X expected %X\n", appHash, hashExp)
		return errors.New("FinalizeBlock  error")
	}
	fmt.Println("Passed test: FinalizeBlock")
	return nil
}
