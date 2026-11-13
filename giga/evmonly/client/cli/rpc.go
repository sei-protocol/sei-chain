package cli

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/spf13/cobra"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

// defaultGasAdjustment scales a gas estimate when --gas-adjustment is not set.
const defaultGasAdjustment = 1.2

// errUnpack is the error of a call whose output does not decode.
var errUnpack = errors.New("unpack")

// receiptTimeout is how long a transaction may take to be included.
var receiptTimeout = time.Minute

// receiptPoll is how often a pending transaction's receipt is polled.
var receiptPoll = 500 * time.Millisecond

// evmClient calls and transacts with the precompiles over EVM JSON-RPC.
type evmClient struct {
	ec *ethclient.Client
	// block is the block calls read, or nil for the latest.
	block *big.Int
}

// dial connects to the --evm-rpc endpoint of an EVM-only chain, reading at
// --height when it is set.
func dial(cmd *cobra.Command) (*evmClient, error) {
	url, err := cmd.Flags().GetString(FlagEVMRPC)
	if err != nil {
		return nil, err
	}
	if url == "" {
		return nil, errNoEndpoint
	}
	ec, err := ethclient.DialContext(cmdContext(cmd), url)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	c := &evmClient{ec: ec}
	if err := c.requireEVMOnly(cmdContext(cmd), url); err != nil {
		return nil, err
	}
	if flagChanged(cmd, flags.FlagHeight) {
		h, err := cmd.Flags().GetInt64(flags.FlagHeight)
		if err != nil {
			return nil, err
		}
		if h > 0 {
			c.block = big.NewInt(h)
		}
	}
	return c, nil
}

// requireEVMOnly returns an error naming --node unless the chain at url serves
// the EVM-only params precompile.
func (c *evmClient) requireEVMOnly(ctx context.Context, url string) error {
	probe := chainparams.Keys()[0]
	_, err := c.call(ctx, chainparams.Address, chainparams.ABI, chainparams.ParamMethod, probe.Subspace, probe.Key)
	var rpcErr rpc.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &rpcErr), errors.Is(err, errUnpack):
		return fmt.Errorf("%s is not an EVM-only chain; use --%s for a chain with a Tendermint RPC", url, flags.FlagNode)
	default:
		return fmt.Errorf("%s: %w", url, err)
	}
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// call calls method on the precompile at to and returns its unpacked outputs.
func (c *evmClient) call(ctx context.Context, to common.Address, contract abi.ABI, method string, args ...any) ([]any, error) {
	m, ok := contract.Methods[method]
	if !ok {
		return nil, fmt.Errorf("unknown method %s", method)
	}
	input, err := m.Inputs.Pack(args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", m.Sig, err)
	}
	out, err := c.ec.CallContract(ctx, ethereum.CallMsg{To: &to, Data: append(m.ID, input...)}, c.block)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.Name, err)
	}
	vals, err := m.Outputs.Unpack(out)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", errUnpack, m.Name, err)
	}
	return vals, nil
}

// txOpts are the gas settings of a transaction; zero values are filled from
// the node.
type txOpts struct {
	gas           uint64
	gasAdjustment float64
	gasPrice      *big.Int
}

// unsignedTx is a legacy transaction ready to sign, with its sender and chain.
type unsignedTx struct {
	from    common.Address
	chainID *big.Int
	tx      *ethtypes.LegacyTx
}

// prepare returns an unsigned legacy transaction from from calling to with data
// and value, with its nonce, gas and gas price filled from the node where o
// leaves them unset.
func (c *evmClient) prepare(ctx context.Context, from, to common.Address, value *big.Int, data []byte, o txOpts) (unsignedTx, error) {
	chainID, err := c.ec.ChainID(ctx)
	if err != nil {
		return unsignedTx{}, fmt.Errorf("chain id: %w", err)
	}
	nonce, err := c.ec.PendingNonceAt(ctx, from)
	if err != nil {
		return unsignedTx{}, fmt.Errorf("nonce: %w", err)
	}
	gasPrice := o.gasPrice
	if gasPrice == nil || gasPrice.Sign() == 0 {
		if gasPrice, err = c.ec.SuggestGasPrice(ctx); err != nil {
			return unsignedTx{}, fmt.Errorf("gas price: %w", err)
		}
	}
	gas := o.gas
	if gas == 0 {
		estimate, err := c.ec.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &to, Value: value, Data: data})
		if err != nil {
			return unsignedTx{}, fmt.Errorf("estimate gas: %w", err)
		}
		gas = adjustGas(estimate, o.gasAdjustment)
	}
	return unsignedTx{from: from, chainID: chainID, tx: &ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gas,
		To:       &to,
		Value:    value,
		Data:     data,
	}}, nil
}

// MarshalJSON returns u's fields as the confirmation prompt shows them.
func (u unsignedTx) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		From     common.Address `json:"from"`
		To       common.Address `json:"to"`
		ChainID  *hexutil.Big   `json:"chainId"`
		Nonce    hexutil.Uint64 `json:"nonce"`
		Gas      hexutil.Uint64 `json:"gas"`
		GasPrice *hexutil.Big   `json:"gasPrice"`
		Value    *hexutil.Big   `json:"value"`
		Input    hexutil.Bytes  `json:"input"`
	}{u.from, *u.tx.To, (*hexutil.Big)(u.chainID), hexutil.Uint64(u.tx.Nonce), hexutil.Uint64(u.tx.Gas), (*hexutil.Big)(u.tx.GasPrice), (*hexutil.Big)(u.tx.Value), u.tx.Data})
}

// sign signs u with key.
func (u unsignedTx) sign(key *ecdsa.PrivateKey) (*ethtypes.Transaction, error) {
	return ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(u.chainID), u.tx)
}

// broadcast sends tx and waits for its receipt.
func (c *evmClient) broadcast(ctx context.Context, tx *ethtypes.Transaction) (*ethtypes.Receipt, error) {
	if err := c.ec.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	return c.waitReceipt(ctx, tx.Hash())
}

func adjustGas(estimate uint64, adjustment float64) uint64 {
	if adjustment <= 0 {
		adjustment = defaultGasAdjustment
	}
	return uint64(float64(estimate) * adjustment)
}

// waitReceipt polls for hash's receipt until it is found or receiptTimeout
// passes.
func (c *evmClient) waitReceipt(ctx context.Context, hash common.Hash) (*ethtypes.Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, receiptTimeout)
	defer cancel()
	ticker := time.NewTicker(receiptPoll)
	defer ticker.Stop()
	for {
		receipt, err := c.ec.TransactionReceipt(ctx, hash)
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, ethereum.NotFound) {
			return nil, fmt.Errorf("receipt %s: %w", hash, err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("transaction %s was not included within %s", hash, receiptTimeout)
		case <-ticker.C:
		}
	}
}
