//go:build inprocess

package inprocess

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
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

// genesisBuilder accumulates per-validator accounts, balances, and gentxs across
// the key-generation pass, then assembles a shared genesis whose validator set
// is left EMPTY so every node derives the consensus valset from its InitChain
// response (the empty-valset invariant), the load-bearing delta from
// testutil/network.
//
// This is a self-contained reimplementation of the unexported initGenFiles /
// collectGenFiles / writeFile helpers in sei-cosmos/testutil/network: lifting
// them verbatim would require exporting them from a production cosmos package.
// They use only exported cosmos APIs, so reimplementing keeps the harness free
// of any sei-cosmos source change.
type genesisBuilder struct {
	codec     codec.Codec
	txConfig  client.TxConfig
	chainID   string
	bondDenom string

	accounts []authtypes.GenesisAccount
	balances []banktypes.Balance
}

// fundValidator stores a validator operator key in kb, funds its genesis account
// + balances, and writes its self-delegation gentx to gentxsDir keyed by moniker.
// It returns the operator address for downstream client wiring.
func (b *genesisBuilder) fundValidator(
	kb keyring.Keyring,
	moniker string,
	pubKey cryptotypes.PubKey,
	algo keyring.SignatureAlgo,
	accountTokens, stakingTokens, bondedTokens sdk.Int,
	p2pHost, p2pPort, nodeID, gentxsDir string,
) (sdk.AccAddress, error) {
	addr, _, err := testutil.GenerateSaveCoinKey(kb, moniker, "", true, algo)
	if err != nil {
		return nil, fmt.Errorf("generate key for %s: %w", moniker, err)
	}

	balances := sdk.NewCoins(
		sdk.NewCoin(fmt.Sprintf("%stoken", moniker), accountTokens),
		sdk.NewCoin(b.bondDenom, stakingTokens),
	)
	b.balances = append(b.balances, banktypes.Balance{Address: addr.String(), Coins: balances.Sort()})
	b.accounts = append(b.accounts, authtypes.NewBaseAccount(addr, nil, 0, 0))

	commission, err := sdk.NewDecFromStr("0.5")
	if err != nil {
		return nil, err
	}
	createValMsg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(addr), pubKey,
		sdk.NewCoin(b.bondDenom, bondedTokens),
		stakingtypes.NewDescription(moniker, "", "", "", ""),
		stakingtypes.NewCommissionRates(commission, sdk.OneDec(), sdk.OneDec()),
		sdk.OneInt(),
	)
	if err != nil {
		return nil, err
	}

	memo := fmt.Sprintf("%s@%s:%s", nodeID, p2pHost, p2pPort)
	txb := b.txConfig.NewTxBuilder()
	if err := txb.SetMsgs(createValMsg); err != nil {
		return nil, err
	}
	txb.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(fmt.Sprintf("%stoken", moniker), sdk.NewInt(0))))
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

// fundAccount stores a non-validator key in kb and funds its genesis account +
// balance. Unlike fundValidator it writes no gentx (the account never stakes) —
// it is the genesis-funded signing account a suite spends from (e.g. `admin`).
func (b *genesisBuilder) fundAccount(
	kb keyring.Keyring,
	name string,
	algo keyring.SignatureAlgo,
	coins sdk.Coins,
) error {
	addr, _, err := testutil.GenerateSaveCoinKey(kb, name, "", true, algo)
	if err != nil {
		return fmt.Errorf("generate key for %s: %w", name, err)
	}
	b.accounts = append(b.accounts, authtypes.NewBaseAccount(addr, nil, 0, 0))
	if !coins.Empty() {
		b.balances = append(b.balances, banktypes.Balance{Address: addr.String(), Coins: coins.Sort()})
	}
	return nil
}

// writeBaseGenesis writes a base genesis file (accounts + balances, empty
// validator set) to every validator's genesis path. Mirrors initGenFiles.
func (b *genesisBuilder) writeBaseGenesis(baseState map[string]json.RawMessage, genFiles []string) error {
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
		ChainID:    b.chainID,
		AppState:   appStateJSON,
		Validators: nil, // empty-valset invariant: derive valset from InitChain.
	}
	for _, gf := range genFiles {
		if err := genDoc.SaveAs(gf); err != nil {
			return err
		}
	}
	return nil
}

// collectGentxs folds every validator's gentx into each node's genesis app state
// under one canonical genesis time (consensus timestamp validation diverges if
// the nodes disagree on GenesisTime). Mirrors collectGenFiles.
func (b *genesisBuilder) collectGentxs(nodes []*node, gentxsDir string) error {
	genTime := tmtime.Now()
	for _, n := range nodes {
		initCfg := genutiltypes.NewInitConfig(b.chainID, gentxsDir, n.nodeID, n.pubKey)
		genFile := n.tmCfg.GenesisFile()
		genDoc, err := tmtypes.GenesisDocFromFile(genFile)
		if err != nil {
			return err
		}
		appState, err := genutil.GenAppStateFromConfig(
			b.codec, b.txConfig, n.tmCfg, initCfg, *genDoc, banktypes.GenesisBalancesIterator{},
		)
		if err != nil {
			return err
		}
		if err := genutil.ExportGenesisFileWithTime(genFile, b.chainID, nil, appState, genTime); err != nil {
			return err
		}
	}
	return nil
}

// writeFile writes contents under dir/name, creating dir. Mirrors the network
// package's unexported writeFile.
func writeFile(name, dir string, contents []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), contents, 0o600)
}
