package cli

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

// defaultGasAdjustment scales a gas estimate when --gas-adjustment is not set.
const defaultGasAdjustment = 1.2

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

// dial connects to the --evm-rpc endpoint, reading at --height when it is set.
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
		return nil, fmt.Errorf("unpack %s: %w", m.Name, err)
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

// send signs and sends a legacy transaction calling to with data and value, and
// waits for its receipt.
func (c *evmClient) send(ctx context.Context, key *ecdsa.PrivateKey, to common.Address, value *big.Int, data []byte, o txOpts) (*ethtypes.Transaction, *ethtypes.Receipt, error) {
	from := crypto.PubkeyToAddress(key.PublicKey)
	chainID, err := c.ec.ChainID(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("chain id: %w", err)
	}
	nonce, err := c.ec.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, nil, fmt.Errorf("nonce: %w", err)
	}
	gasPrice := o.gasPrice
	if gasPrice == nil || gasPrice.Sign() == 0 {
		if gasPrice, err = c.ec.SuggestGasPrice(ctx); err != nil {
			return nil, nil, fmt.Errorf("gas price: %w", err)
		}
	}
	gas := o.gas
	if gas == 0 {
		estimate, err := c.ec.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &to, Value: value, Data: data})
		if err != nil {
			return nil, nil, fmt.Errorf("estimate gas: %w", err)
		}
		gas = adjustGas(estimate, o.gasAdjustment)
	}
	tx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(chainID), &ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gas,
		To:       &to,
		Value:    value,
		Data:     data,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := c.ec.SendTransaction(ctx, tx); err != nil {
		return nil, nil, fmt.Errorf("send: %w", err)
	}
	receipt, err := c.waitReceipt(ctx, tx.Hash())
	if err != nil {
		return tx, nil, err
	}
	return tx, receipt, nil
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
