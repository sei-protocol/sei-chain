package chain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Per-validator genesis funding, in consensus power at the default power
// reduction. The operator is funded above what it bonds so it can pay fees.
const (
	validatorAccountPower = 1000
	validatorBondedPower  = 100
)

// genesisBuilder accumulates accounts, balances, and gentxs across the
// provisioning pass, then assembles the genesis doc every validator loads.
//
// It reimplements the unexported initGenFiles/collectGenFiles helpers in
// sei-cosmos/testutil/network rather than lifting them, which would require
// exporting them from a production cosmos package. They use only exported cosmos
// APIs, so the engine needs no sei-cosmos source change.
type genesisBuilder struct {
	codec     codec.Codec
	txConfig  client.TxConfig
	chainID   string
	bondDenom string

	accounts []authtypes.GenesisAccount
	balances []banktypes.Balance
}

// signingAlgo resolves the secp256k1 signing algorithm the keyring supports.
func signingAlgo(kb keyring.Keyring) (keyring.SignatureAlgo, error) {
	algos, _ := kb.SupportedAlgorithms()
	return keyring.NewSigningAlgoFromString(string(hd.Secp256k1Type), algos)
}

// consensusTokens converts a consensus power to a token amount at the default
// power reduction.
func consensusTokens(power int64) sdk.Int {
	return sdk.TokensFromConsensusPower(power, sdk.DefaultPowerReduction)
}

// fundValidator stores a validator operator key in kb, funds its genesis
// account, and writes its self-delegation gentx to gentxsDir keyed by moniker.
// The gentx memo carries nodeID@host:port, which is what collectGentxs later
// derives the peer mesh from.
func (b *genesisBuilder) fundValidator(
	kb keyring.Keyring,
	moniker string,
	pubKey cryptotypes.PubKey,
	algo keyring.SignatureAlgo,
	p2pHost, p2pPort, nodeID, gentxsDir string,
) (sdk.AccAddress, error) {
	addr, _, err := testutil.GenerateSaveCoinKey(kb, moniker, "", true, algo)
	if err != nil {
		return nil, fmt.Errorf("generate key for %s: %w", moniker, err)
	}
	b.accounts = append(b.accounts, authtypes.NewBaseAccount(addr, nil, 0, 0))
	b.balances = append(b.balances, banktypes.Balance{
		Address: addr.String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(b.bondDenom, consensusTokens(validatorAccountPower))),
	})

	commission, err := sdk.NewDecFromStr("0.5")
	if err != nil {
		return nil, err
	}
	createVal, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(addr), pubKey,
		sdk.NewCoin(b.bondDenom, consensusTokens(validatorBondedPower)),
		stakingtypes.NewDescription(moniker, "", "", "", ""),
		stakingtypes.NewCommissionRates(commission, sdk.OneDec(), sdk.OneDec()),
		sdk.OneInt(),
	)
	if err != nil {
		return nil, err
	}

	memo := fmt.Sprintf("%s@%s:%s", nodeID, p2pHost, p2pPort)
	txb := b.txConfig.NewTxBuilder()
	if err := txb.SetMsgs(createVal); err != nil {
		return nil, err
	}
	txb.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(b.bondDenom, sdk.ZeroInt())))
	txb.SetGasLimit(1_000_000)
	txb.SetMemo(memo)
	txf := tx.Factory{}.WithChainID(b.chainID).WithMemo(memo).WithKeybase(kb).WithTxConfig(b.txConfig)
	if err := tx.Sign(txf, moniker, txb, true); err != nil {
		return nil, err
	}
	txBz, err := b.txConfig.TxJSONEncoder()(txb.GetTx())
	if err != nil {
		return nil, err
	}
	if err := writeFile(moniker+".json", gentxsDir, txBz); err != nil {
		return nil, err
	}
	return addr, nil
}

// fundAccount stores a non-validator key in kb and funds its genesis account. It
// writes no gentx: the account never stakes, it is a signing account a suite
// spends from.
func (b *genesisBuilder) fundAccount(kb keyring.Keyring, name string, algo keyring.SignatureAlgo, coins sdk.Coins) (sdk.AccAddress, error) {
	addr, _, err := testutil.GenerateSaveCoinKey(kb, name, "", true, algo)
	if err != nil {
		return nil, fmt.Errorf("generate key for %s: %w", name, err)
	}
	b.accounts = append(b.accounts, authtypes.NewBaseAccount(addr, nil, 0, 0))
	if !coins.Empty() {
		b.balances = append(b.balances, banktypes.Balance{Address: addr.String(), Coins: coins.Sort()})
	}
	return addr, nil
}

// provisionGenesisAccounts creates every Config.GenesisAccounts key in the
// chain-wide keyring and funds it at genesis. It runs after provisionValidators
// (the keyring exists) and before genesis assembly (the balances fold in).
func (c *Chain) provisionGenesisAccounts(gb *genesisBuilder) error {
	algo, err := signingAlgo(c.keyring)
	if err != nil {
		return err
	}
	for _, ga := range c.cfg.GenesisAccounts {
		if _, err := gb.fundAccount(c.keyring, ga.Name, algo, ga.Coins); err != nil {
			return fmt.Errorf("provision genesis account %q: %w", ga.Name, err)
		}
	}
	return nil
}

// writeBaseGenesis writes the pre-gentx genesis doc to every validator's genesis
// path: default module state plus the accumulated accounts and balances.
//
// genesisTime is pinned here, not left to ValidateAndComplete, because every
// validator must agree on it or consensus timestamp validation diverges.
// consensusParams is carried through as given — see assertGenesisConsensusParams.
func (b *genesisBuilder) writeBaseGenesis(
	baseState map[string]json.RawMessage,
	consensusParams *tmtypes.ConsensusParams,
	genFiles []string,
) error {
	var authGenState authtypes.GenesisState
	b.codec.MustUnmarshalJSON(baseState[authtypes.ModuleName], &authGenState)
	packed, err := authtypes.PackAccounts(b.accounts)
	if err != nil {
		return err
	}
	authGenState.Accounts = append(authGenState.Accounts, packed...)
	baseState[authtypes.ModuleName] = b.codec.MustMarshalJSON(&authGenState)

	var bankGenState banktypes.GenesisState
	b.codec.MustUnmarshalJSON(baseState[banktypes.ModuleName], &bankGenState)
	bankGenState.Balances = append(bankGenState.Balances, b.balances...)
	baseState[banktypes.ModuleName] = b.codec.MustMarshalJSON(&bankGenState)

	appStateJSON, err := json.MarshalIndent(baseState, "", "  ")
	if err != nil {
		return err
	}
	genDoc := tmtypes.GenesisDoc{
		GenesisTime:     tmtime.Now(),
		ChainID:         b.chainID,
		AppState:        appStateJSON,
		Validators:      nil, // empty-valset: derive the valset from InitChain.
		ConsensusParams: consensusParams,
	}
	for _, gf := range genFiles {
		if err := genDoc.SaveAs(gf); err != nil {
			return err
		}
	}
	return nil
}

// collectGentxs folds every validator's gentx into each validator's genesis app
// state, and is also what wires the P2P mesh: genutil.GenAppStateFromConfig
// derives PersistentPeers from the gentx memos and writes it, in place, onto the
// very *config.Config the engine holds and later hands to tmnode.New.
//
// That in-place mutation is invisible at this layer and fragile — cloning a
// config before this step, or building nodes before collecting, drops consensus
// silently — which is why assertPeerMesh runs immediately after. Note also that
// GenAppStateFromConfig writes the genesis file itself from the doc passed in,
// so it preserves the consensus params the doc already carries.
func (b *genesisBuilder) collectGentxs(validators []*Validator, gentxsDir string) error {
	for _, v := range validators {
		initCfg := genutiltypes.NewInitConfig(b.chainID, gentxsDir, v.nodeID, v.pubKey)
		genDoc, err := tmtypes.GenesisDocFromFile(v.tmCfg.GenesisFile())
		if err != nil {
			return err
		}
		if _, err := genutil.GenAppStateFromConfig(
			b.codec, b.txConfig, v.tmCfg, initCfg, *genDoc, banktypes.GenesisBalancesIterator{},
		); err != nil {
			return err
		}
	}
	return nil
}

// writeFile writes contents under dir/name, creating dir.
func writeFile(name, dir string, contents []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), contents, 0o600)
}
