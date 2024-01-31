package processblock

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/go-bip39"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"
)

type App struct {
	*app.App

	height        int64
	proposer      int
	accToMnemonic map[string]string
	accToSeqDelta map[string]uint64
	lastCtx       sdk.Context
}

func NewTestApp() *App {
	a := &App{
		App:           app.Setup(false, false),
		height:        1,
		accToMnemonic: map[string]string{},
		accToSeqDelta: map[string]uint64{},
	}
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	cp := tmtypes.DefaultConsensusParams().ToProto()
	gs := app.NewDefaultGenesisState(marshaler)
	gbz, err := json.Marshal(gs)
	if err != nil {
		panic(err)
	}
	_, err = a.InitChain(context.Background(), &types.RequestInitChain{
		Time:            time.Now(),
		ChainId:         "tendermint_test",
		ConsensusParams: &cp,
		Validators:      []types.ValidatorUpdate{},
		InitialHeight:   1,
		AppStateBytes:   gbz,
	})
	if err != nil {
		panic(err)
	}
	a.lastCtx = a.GetContextForDeliverTx([]byte{})
	return a
}

func (a *App) Ctx() sdk.Context {
	return a.lastCtx
}

// Processes and commits a block of transactions, and return a list of response codes.
// Assumes all validators voted with equal weight, and there are no byzantine validators.
// Proposer is rotated among all validators round-robin.
func (a *App) RunBlock(txs []signing.Tx) (resultCodes []uint32) {
	defer func() {
		a.lastCtx = a.GetContextForDeliverTx([]byte{}) // Commit will set deliver tx ctx to nil so we need to cache it here for testing queries before the next block is FinalizeBlock'ed (which will set deliver tx ctx)
		_, err := a.Commit(context.Background())
		if err != nil {
			panic(err)
		}
		a.accToSeqDelta = map[string]uint64{}
		a.height++
		a.proposer = (a.proposer + 1) % len(a.GetAllValidators())
	}()

	res, err := a.FinalizeBlock(context.Background(), &types.RequestFinalizeBlock{
		Txs: utils.Map(txs, func(tx signing.Tx) []byte {
			bz, err := TxConfig.TxEncoder()(tx)
			if err != nil {
				panic(err)
			}
			return bz
		}),
		DecidedLastCommit: types.CommitInfo{
			Round: 0,
			Votes: a.GetVotes(),
		},
		ByzantineValidators: []types.Misbehavior{},
		Hash:                []byte("abc"), // no needed for application logic
		Height:              a.height,
		ProposerAddress:     getValAddress(a.GetProposer()),
		Time:                time.Now(),
	})
	if err != nil {
		panic(err)
	}
	return utils.Map(res.TxResults, func(r *types.ExecTxResult) uint32 { return r.Code })
}

func (a *App) GetVotes() []types.VoteInfo {
	return utils.Map(a.GetAllValidators(), func(v stakingtypes.Validator) types.VoteInfo {
		return types.VoteInfo{
			Validator: types.Validator{
				Address: getValAddress(v),
				Power:   1,
			},
			SignedLastBlock: true,
		}
	})
}

func (a *App) GetAllValidators() []stakingtypes.Validator {
	return a.StakingKeeper.GetAllValidators(a.Ctx())
}

func (a *App) GetProposer() stakingtypes.Validator {
	return a.GetAllValidators()[a.proposer]
}

func (a *App) GenerateSignableKey(_ string) (addr sdk.AccAddress) {
	entropySeed, err := bip39.NewEntropy(256)
	if err != nil {
		panic(err)
	}
	mnemonic, err := bip39.NewMnemonic(entropySeed)
	if err != nil {
		panic(err)
	}
	hdPath := hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String()
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", hdPath)
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	addr = sdk.AccAddress(privKey.PubKey().Address())
	a.accToMnemonic[addr.String()] = mnemonic
	return
}

func GenerateRandomPubKey() cryptotypes.PubKey {
	pubBz := make([]byte, secp256k1.PubKeySize)
	pub := &secp256k1.PubKey{Key: pubBz}
	if _, err := rand.Read(pub.Key); err != nil {
		panic(err)
	}
	return pub
}

func generateRandomStringOfLength(len int) string {
	bz := make([]byte, len)
	if _, err := rand.Read(bz); err != nil {
		panic(err)
	}
	return string(bz)
}

func getValAddress(v stakingtypes.Validator) []byte {
	pub := secp256k1.PubKey{}
	if err := pub.Unmarshal(v.ConsensusPubkey.Value); err != nil {
		panic(err)
	}
	return sdk.AccAddress(pub.Address())
}
