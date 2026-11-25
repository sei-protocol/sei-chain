package p2p

import (
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type gigaEvmTestFixture struct {
	Proxy  *abevm.Proxy
	Router *GigaRouterCommon
	common *GigaRouterCommon
}

type evmStateApp struct{ abevm.Application }

func (a evmStateApp) EvmChainID() uint64 { return 1 }

func (a evmStateApp) LastBlockHeight() int64                       { return 17 }
func (a evmStateApp) EvmNonce(common.Address) uint64               { return 3 }
func (a evmStateApp) EvmCommittedNonce(addr common.Address) uint64 { return a.EvmNonce(addr) }

func signedEVMStateTestTx(t *testing.T, gas uint64) *ethtypes.Transaction {
	t.Helper()
	return signEVMStateTestTx(t, ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 3, Gas: gas}), ethtypes.LatestSignerForChainID(big.NewInt(1)))
}

func signEVMStateTestTx(t *testing.T, tx *ethtypes.Transaction, signer ethtypes.Signer) *ethtypes.Transaction {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := ethtypes.SignTx(tx, signer, key)
	require.NoError(t, err)
	return signed
}

// senderCacheApp records the cached sender delivered to admission.
type senderCacheApp struct {
	evmStateApp
	sender   utils.Option[common.Address]
	checkErr utils.Option[error]
}

func (a *senderCacheApp) CheckTx(tx abevm.EvmTx) (abevm.AdmittedTx, error) {
	a.sender = utils.Some(tx.Sender())
	if err, ok := a.checkErr.Get(); ok {
		return abevm.AdmittedTx{}, err
	}
	return abevm.AdmittedTx{}, nil
}

type broadcastEVMApp struct{ abevm.Application }

func (broadcastEVMApp) LastBlockHeight() int64 { return 0 }

// EvmNonce is the nonce the producer reports before its mempool is running.
func (broadcastEVMApp) EvmNonce(common.Address) uint64 { return 7 }

func newAutobahnBroadcastEnv(t *testing.T, rng utils.Rng) *gigaEvmTestFixture {
	t.Helper()
	return newAutobahnEnv(t, rng, true)
}

func newAutobahnFullnodeEnv(t *testing.T, rng utils.Rng) *gigaEvmTestFixture {
	t.Helper()
	return newAutobahnEnv(t, rng, false)
}

func newAutobahnEnv(t *testing.T, rng utils.Rng, validator bool) *gigaEvmTestFixture {
	t.Helper()
	return newAutobahnEnvWithApp(t, rng, validator, abevm.NewProxy(broadcastEVMApp{}))
}

func newAutobahnEnvWithApp(t *testing.T, rng utils.Rng, validator bool, app *abevm.Proxy) *gigaEvmTestFixture {
	t.Helper()
	return newAutobahnEnvWithSigner(t, rng, validator, app, newTestSigner(app.EvmChainID(), 0))
}

func newAutobahnEnvWithSigner(t *testing.T, rng utils.Rng, validator bool, app *abevm.Proxy, signer *abevm.Signer) *gigaEvmTestFixture {
	t.Helper()
	return newAutobahnRoutingEnv(t, rng, validator, app, signer, utils.None[common.Address]())
}

func newAutobahnProxyEnv(t *testing.T, rng utils.Rng, validator bool, address common.Address, client *ethrpc.Client) *gigaEvmTestFixture {
	t.Helper()
	app := abevm.NewProxy(broadcastEVMApp{})
	env := newAutobahnRoutingEnv(t, rng, validator, app, newTestSigner(app.EvmChainID(), 0), utils.Some(address))
	owner := env.common.data.Anchor().Load().NextHeightEpoch().Committee().EvmShard(address)
	for proxies := range env.common.proxies.Lock() {
		proxies[owner] = client
	}
	return env
}

func newAutobahnRoutingEnv(t *testing.T, rng utils.Rng, validator bool, app *abevm.Proxy, signer *abevm.Signer, remoteSender utils.Option[common.Address]) *gigaEvmTestFixture {
	t.Helper()
	size := 1
	if remoteSender.IsPresent() {
		size = 2
	}
	committee, keys := atypes.GenCommittee(rng, size)
	valKey := keys[0]
	if address, ok := remoteSender.Get(); ok && committee.EvmShard(address) == valKey.Public() {
		valKey = keys[1]
	}
	nodeKey := NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32)))
	genDoc := &types.GenesisDoc{
		ChainID:         "rpc-autobahn",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: types.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	addrs := map[atypes.PublicKey]GigaNodeAddr{
		valKey.Public(): {
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 26657},
		},
	}
	blockStore := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, blockStore.Close()) })
	commonCfg := GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     addrs,
		PersistentStateDir: t.TempDir(),
		App:                app,
		AppHashBuilder:     newManualAppHashBuilder(),
		GenDoc:             genDoc,
		EnableEvmProxy:     true,
	}
	registry, err := epoch.TestGenesisRegistry(committee, atypes.RoadHeight(genDoc.InitialHeight), epoch.DefaultEpochLength)
	require.NoError(t, err)
	dataState, err := BuildDataState(&commonCfg, blockStore, abevm.TestEpochApp(registry), genDoc.GenesisTime)
	require.NoError(t, err)
	var giga GigaRouter
	var common *GigaRouterCommon
	if validator {
		router, err := NewGigaValidatorRouter(&GigaValidatorConfig{
			EVMRPC:                 url.URL{Scheme: "ws", Host: "127.0.0.1:8546"},
			GigaRouterCommonConfig: commonCfg,
			ValidatorKey:           valKey,
			ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
			ProposalTimeout:        time.Hour,
			Producer: &producer.Config{
				MaxGasWantedPerBlock:    1,
				MaxGasEstimatedPerBlock: 1,
				MaxTxsPerBlock:          1,
				MaxTxsPerSecond:         utils.None[uint64](),
				BlockInterval:           time.Second,
			},
		}, nodeKey, dataState, signer)
		require.NoError(t, err)
		require.True(t, router.Mempool().IsPresent(), "validator GigaRouter must expose a mempool so a regression would wait, not take the fullnode shortcut")
		giga = router
		common = router.GigaRouterCommon
	} else {
		router, err := NewGigaFullnodeRouter(&commonCfg, nodeKey, dataState, signer)
		require.NoError(t, err)
		require.False(t, router.Mempool().IsPresent())
		giga = router
		common = router.GigaRouterCommon
	}
	return &gigaEvmTestFixture{Proxy: app, Router: giga.Common(), common: common}
}

func (broadcastEVMApp) EvmChainID() uint64 { return 1 }
