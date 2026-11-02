package v2record

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity/capture"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const (
	cosmosChainID = "sei-test"
	blockInterval = 400 * time.Millisecond
	// settleTimeout bounds the wait for the receipt store to index the
	// committed blocks.
	settleTimeout = 30 * time.Second
)

var (
	// genesisTime carries sub-second precision so that milliTimestamp and
	// timestamp differ.
	genesisTime = time.Date(2026, time.January, 2, 3, 4, 5, 123456789, time.UTC)
	proposer    = common.HexToAddress("0x00000000000000000000000000000000000b10c5")
	// fundingUsei is each account's starting balance.
	fundingUsei = sdk.NewInt(10_000_000_000)
)

// v2Chain is an in-process v2 node: an app that finalizes blocks under headers
// chosen here, and the v2 EVM RPC serving them.
type v2Chain struct {
	t       *testing.T
	app     *app.App
	client  *chainClient
	signer  ethtypes.Signer
	chainID *big.Int
	nonces  map[common.Address]uint64
	fixture *parity.Fixture
	// rpc is the v2 EVM RPC over the chain, once serve has started it.
	rpc parity.Caller
}

// newV2Chain starts a chain whose first block funds accounts, and a fixture
// whose prestate is the EVM state that block leaves.
func newV2Chain(t *testing.T, name, description string, accounts []*ecdsa.PrivateKey) *v2Chain {
	a := app.Setup(t, false, true, false)
	a.ChainID = cosmosChainID
	c := &v2Chain{t: t, app: a, client: &chainClient{}, nonces: map[common.Address]uint64{}}
	c.finalize(nil, func(ctx sdk.Context) {
		for _, key := range accounts {
			c.fund(ctx, key)
		}
	})
	ctx := c.latestCtx()
	c.chainID = a.EvmKeeper.ChainID(ctx)
	c.signer = ethtypes.LatestSignerForChainID(c.chainID)
	c.fixture = &parity.Fixture{
		Name:        name,
		Source:      "synthetic",
		Description: description,
		ChainID:     hexutil.Uint64(c.chainID.Uint64()),
		Prestate:    map[common.Address]parity.Account{},
	}
	coinbase, err := a.EvmKeeper.GetFeeCollectorAddress(ctx)
	require.NoError(t, err)
	for _, address := range append(addresses(accounts), coinbase) {
		c.fixture.Prestate[address] = parity.Account{
			Balance: (*hexutil.Big)(a.EvmKeeper.GetBalance(ctx, a.EvmKeeper.GetSeiAddressOrDefault(ctx, address))),
			Nonce:   hexutil.Uint64(a.EvmKeeper.GetNonce(ctx, address)),
		}
	}
	return c
}

// fund associates key's addresses and credits fundingUsei to them.
func (c *v2Chain) fund(ctx sdk.Context, key *ecdsa.PrivateKey) {
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(&secp256k1.PrivKey{Key: crypto.FromECDSA(key)})
	c.app.EvmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	amount := sdk.NewCoins(sdk.NewCoin("usei", fundingUsei))
	require.NoError(c.t, c.app.BankKeeper.MintCoins(ctx, evmtypes.ModuleName, amount))
	require.NoError(c.t, c.app.BankKeeper.SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amount))
}

// block signs txs with each sender's next nonce, finalizes them as the next
// block, and adds the block to the fixture with the context v2 executes it under.
func (c *v2Chain) block(txs ...signedTx) []*ethtypes.Transaction {
	ctx := c.latestCtx()
	coinbase, err := c.app.EvmKeeper.GetFeeCollectorAddress(ctx)
	require.NoError(c.t, err)
	baseFee := c.app.EvmKeeper.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()

	var cosmosTxs [][]byte
	raw := []hexutil.Bytes{}
	var signed []*ethtypes.Transaction
	for _, tx := range txs {
		sender := crypto.PubkeyToAddress(tx.key.PublicKey)
		ethTx, err := ethtypes.SignNewTx(tx.key, c.signer, tx.data(c.nonces[sender], c.chainID))
		require.NoError(c.t, err)
		c.nonces[sender]++
		bz, err := ethTx.MarshalBinary()
		require.NoError(c.t, err)
		raw = append(raw, bz)
		cosmosTxs = append(cosmosTxs, c.cosmosTx(ethTx))
		signed = append(signed, ethTx)
	}
	header := c.finalize(cosmosTxs, nil)
	prevRandao, err := header.Time.MarshalBinary()
	require.NoError(c.t, err)
	c.fixture.Blocks = append(c.fixture.Blocks, parity.Block{
		Header: parity.Header{Number: hexutil.Uint64(header.Height), Time: header.Time}, //nolint:gosec // heights are positive.
		Execution: parity.Execution{
			Coinbase:    coinbase,
			PrevRandao:  crypto.Keccak256Hash(prevRandao),
			BaseFee:     (*hexutil.Big)(baseFee),
			BlobBaseFee: (*hexutil.Big)(big.NewInt(1)),
		},
		Txs: raw,
	})
	return signed
}

// cosmosTx wraps tx in the Cosmos envelope a v2 block carries.
func (c *v2Chain) cosmosTx(tx *ethtypes.Transaction) []byte {
	var typed proto.Message
	var err error
	switch tx.Type() {
	case ethtypes.LegacyTxType:
		typed, err = ethtx.NewLegacyTx(tx)
	case ethtypes.AccessListTxType:
		typed, err = ethtx.NewAccessListTx(tx)
	case ethtypes.DynamicFeeTxType:
		typed, err = ethtx.NewDynamicFeeTx(tx)
	default:
		err = fmt.Errorf("unsupported tx type %d", tx.Type())
	}
	require.NoError(c.t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(typed)
	require.NoError(c.t, err)
	builder := c.app.GetTxConfig().NewTxBuilder()
	require.NoError(c.t, builder.SetMsgs(msg))
	bz, err := c.app.GetTxConfig().TxEncoder()(builder.GetTx())
	require.NoError(c.t, err)
	return bz
}

// finalize executes and commits txs as the next block, running initialize
// against the block's state before the commit, and returns the block's header.
func (c *v2Chain) finalize(txs [][]byte, initialize func(sdk.Context)) *tmtypes.Header {
	height := c.client.latest() + 1
	header := c.header(height)
	res, err := c.app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Txs:    txs,
		Hash:   header.Hash(),
		Header: &tmproto.Header{ChainID: cosmosChainID, Height: height, Time: header.Time, LastBlockId: header.LastBlockID.ToProto()},
	})
	require.NoError(c.t, err)
	if initialize != nil {
		initialize(c.app.GetContextForDeliverTx(nil))
	}
	_, err = c.app.Commit(context.Background())
	require.NoError(c.t, err)
	blockTxs := make(tmtypes.Txs, len(txs))
	for i, tx := range txs {
		blockTxs[i] = tx
	}
	c.client.record(&coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: header.Hash()},
		Block: &tmtypes.Block{
			Header:     *header,
			Data:       tmtypes.Data{Txs: blockTxs},
			LastCommit: &tmtypes.Commit{Height: height - 1},
		},
	}, res.TxResults, res.ConsensusParamUpdates)
	return header
}

// header returns the header of the block at height, chained to the previous
// one, with every hash field set.
func (c *v2Chain) header(height int64) *tmtypes.Header {
	field := func(name string) []byte { return crypto.Keccak256([]byte(fmt.Sprintf("%s-%d", name, height))) }
	header := &tmtypes.Header{
		ChainID: cosmosChainID,
		Height:  height,
		Time:    genesisTime.Add(time.Duration(height) * blockInterval),
		// Not the app's hash: its genesis is not reproducible, and a fixture
		// must record the same hashes on every run.
		AppHash:         field("app"),
		ProposerAddress: proposer.Bytes(),
		LastCommitHash:  field("last-commit"),
		DataHash:        field("data"),
		// A header without a validators hash has no hash of its own.
		ValidatorsHash:     field("validators"),
		NextValidatorsHash: field("next-validators"),
		ConsensusHash:      field("consensus"),
		LastResultsHash:    field("results"),
		EvidenceHash:       field("evidence"),
	}
	if height > 1 {
		header.LastBlockID = c.client.blocks[height-2].BlockID
	}
	return header
}

func (c *v2Chain) latestCtx() sdk.Context {
	return c.app.RPCContextProvider(evmrpc.LatestCtxHeight)
}

// record completes the fixture's headers from v2's responses and records v2's
// response to every request.
func (c *v2Chain) record(calls []parity.Call) *parity.Fixture {
	caller := c.serve()
	ctx := c.t.Context()
	for i := range c.fixture.Blocks {
		block := &c.fixture.Blocks[i]
		response, err := caller(ctx, "eth_getBlockByNumber", []json.RawMessage{mustJSON(c.t, block.Header.Number), mustJSON(c.t, false)})
		require.NoError(c.t, err)
		require.Nil(c.t, response.Error, "v2 eth_getBlockByNumber(%d)", block.Header.Number)
		var reported struct {
			parity.Header
			GasLimit hexutil.Uint64 `json:"gasLimit"`
		}
		require.NoError(c.t, json.Unmarshal(response.Result, &reported))
		reported.Time = block.Header.Time
		block.Header = reported.Header
		block.Execution.GasLimit = reported.GasLimit
	}
	require.NoError(c.t, parity.Record(ctx, c.fixture, caller, parity.RecordOptions{AtTip: true, Calls: calls}))
	c.requireReceipts()
	return c.fixture
}

// requireReceipts fails unless v2 recorded a receipt for every fixture
// transaction, which it does not for one rejected before execution.
func (c *v2Chain) requireReceipts() {
	for _, request := range c.fixture.Requests {
		if request.Method != "eth_getTransactionReceipt" || string(request.V2.Result) != "null" {
			continue
		}
		var hash common.Hash
		require.NoError(c.t, json.Unmarshal(request.Params[0], &hash))
		if hash == parity.UnknownHash {
			continue
		}
		c.t.Fatalf("v2 has no receipt for %s; tx results: %s", hash, c.txLogs())
	}
}

func (c *v2Chain) txLogs() string {
	var logs []string
	for _, result := range c.client.results {
		for _, tx := range result.TxsResults {
			logs = append(logs, fmt.Sprintf("height %d code %d: %s", result.Height, tx.Code, tx.Log))
		}
	}
	return fmt.Sprint(logs)
}

// capture records count blocks from start as paritycapture records them from
// a live node, so the capture path is replayed alongside the recorded fixture.
func (c *v2Chain) capture(name string, start, count uint64) *parity.Fixture {
	src := capture.Source{EVM: c.serve(), Consensus: c.consensusBlock, Coinbase: keeper.GetCoinbaseAddress()}
	fixture, err := capture.Fixture(c.t.Context(), src, name, start, count)
	require.NoError(c.t, err)
	return fixture
}

func (c *v2Chain) consensusBlock(_ context.Context, height uint64) (capture.ConsensusBlock, error) {
	block, err := c.client.Block(context.Background(), new(int64(height))) //nolint:gosec // heights fit int64.
	if err != nil {
		return capture.ConsensusBlock{}, err
	}
	return capture.ConsensusBlock{Time: block.Block.Time, Txs: len(block.Block.Txs)}, nil
}

// serve starts the v2 EVM RPC over the chain on first use and returns a
// Caller for it. Blocks finalized after it starts are not settled.
func (c *v2Chain) serve() parity.Caller {
	if c.rpc != nil {
		return c.rpc
	}
	port := freePort(c.t)
	cfg := evmrpcconfig.DefaultConfig
	cfg.HTTPEnabled = true
	cfg.HTTPPort = port
	a := c.app
	server, err := evmrpc.NewEVMHTTPServer(cfg, c.client, &a.EvmKeeper, a.BeginBlockKeepers, a.BaseApp, a.TracerAnteHandler,
		a.RPCContextProvider, func(int64) client.TxConfig { return a.GetTxConfig() }, "", a.GetStateStore(), false, nil)
	require.NoError(c.t, err)
	c.settle()
	require.NoError(c.t, server.Start())
	c.t.Cleanup(server.Stop)
	c.rpc = parity.HTTPCaller(http.DefaultClient, fmt.Sprintf("http://127.0.0.1:%d", port))
	return c.rpc
}

// settle waits until the state and receipt stores have applied every
// committed block, and pins the receipt store's window to the chain.
func (c *v2Chain) settle() {
	latest := c.latestCtx().BlockHeight()
	if store := c.app.GetStateStore(); store != nil {
		if waiter, ok := store.(dbtypes.PendingWriteWaiter); ok {
			waiter.WaitForPendingWrites()
		}
		if store.GetLatestVersion() < latest {
			require.NoError(c.t, store.SetLatestVersion(latest))
		}
	}
	store := c.app.EvmKeeper.ReceiptStore()
	require.NotNil(c.t, store)
	require.Eventually(c.t, func() bool { return store.LatestVersion() >= latest }, settleTimeout, time.Millisecond)
	require.NoError(c.t, receipt.PinVersions(store, 1, store.LatestVersion()))
}

func freePort(t *testing.T) int {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	bz, err := json.Marshal(value)
	require.NoError(t, err)
	return bz
}

func addresses(keys []*ecdsa.PrivateKey) []common.Address {
	out := make([]common.Address, len(keys))
	for i, key := range keys {
		out[i] = crypto.PubkeyToAddress(key.PublicKey)
	}
	return out
}
