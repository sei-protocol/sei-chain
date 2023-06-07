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
		App:           app.Setup(false),
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

func (a *App) RunBlock(txs [][]byte) (resultCodes []uint32) {
	defer func() {
		a.lastCtx = a.Ctx()
		_, err := a.Commit(context.Background())
		if err != nil {
			panic(err)
		}
		a.accToSeqDelta = map[string]uint64{}
		a.height++
		a.proposer = (a.proposer + 1) % len(a.GetAllValidators())
	}()

	res, err := a.FinalizeBlock(context.Background(), &types.RequestFinalizeBlock{
		Txs: txs,
		DecidedLastCommit: types.CommitInfo{
			Round: 0,
			Votes: utils.Map(a.GetAllValidators(), func(v stakingtypes.Validator) types.VoteInfo {
				return types.VoteInfo{
					Validator: types.Validator{
						Address: getValAddress(v),
						Power:   1,
					},
					SignedLastBlock: true,
				}
			}),
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

func (a *App) GetAllValidators() []stakingtypes.Validator {
	return a.StakingKeeper.GetAllValidators(a.Ctx())
}

func (a *App) GetProposer() stakingtypes.Validator {
	return a.GetAllValidators()[a.proposer]
}

func (a *App) GenerateSignableKey(name string) (addr sdk.AccAddress) {
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
