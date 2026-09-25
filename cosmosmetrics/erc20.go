package cosmosmetrics

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.opentelemetry.io/otel/attribute"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// EVMKeeper is the EVM state the reporter reads.
type EVMKeeper interface {
	GetEVMAddressOrDefault(sdk.Context, sdk.AccAddress) common.Address
	StaticCallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, data []byte) ([]byte, error)
}

// erc20ABI is the read-only ERC-20 surface the reporter calls.
var erc20ABI = must(abi.JSON(strings.NewReader(`[
	{"name":"balanceOf","type":"function","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"type":"uint256"}]},
	{"name":"decimals","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint8"}]},
	{"name":"symbol","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"string"}]}
]`)))

// erc20Token is a token's contract address and the metadata read from it.
type erc20Token struct {
	address common.Address
	symbol  string
	scale   float64
}

func parseERC20Tokens(addrs []string) ([]common.Address, error) {
	tokens := make([]common.Address, 0, len(addrs))
	for _, a := range addrs {
		if !common.IsHexAddress(a) {
			return nil, fmt.Errorf("%s: %q: not a 0x address", flagERC20Tokens, a)
		}
		tokens = append(tokens, common.HexToAddress(a))
	}
	return tokens, nil
}

// readERC20Balances reports every configured token's balance of every wallet. The wallet's EVM
// address is the chain's own mapping, so an association made after the wallet was configured is
// followed without a configuration change.
func (r *Reporter) readERC20Balances(ctx sdk.Context, b *builder) {
	if len(r.erc20Tokens) == 0 || len(r.wallets) == 0 {
		return
	}
	tokens := make([]erc20Token, 0, len(r.erc20Tokens))
	for _, addr := range r.erc20Tokens {
		token, err := r.readERC20Token(ctx, addr)
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("erc20 %s: %w", addr, err))
			continue
		}
		tokens = append(tokens, token)
	}
	for _, acc := range r.wallets {
		evmAddr := r.keepers.EVM.GetEVMAddressOrDefault(ctx, acc)
		attrs := []attribute.KeyValue{addressAttr(acc.String()), attribute.String("evm_address", evmAddr.Hex())}
		for _, token := range tokens {
			balance, err := r.erc20Call(ctx, acc, token.address, "balanceOf", evmAddr)
			if err != nil {
				b.errs = append(b.errs, fmt.Errorf("wallet %s: erc20 %s balanceOf: %w", acc, token.address, err))
				continue
			}
			amount, ok := balance[0].(*big.Int)
			if !ok {
				b.errs = append(b.errs, fmt.Errorf("wallet %s: erc20 %s balanceOf: unexpected return %T", acc, token.address, balance[0]))
				continue
			}
			b.int(cosmosMetrics.walletERC20Balance, sdk.NewIntFromBigInt(amount), token.scale,
				append(attrs, attribute.String("token", token.address.Hex()), attribute.String("symbol", token.symbol))...)
		}
	}
}

// readERC20Token reads a token's symbol and decimals. Metadata is read every refresh rather than
// cached: it is two static calls per token, and a contract upgraded in place then reports correctly.
func (r *Reporter) readERC20Token(ctx sdk.Context, addr common.Address) (erc20Token, error) {
	from := r.wallets[0]
	decimals, err := r.erc20Call(ctx, from, addr, "decimals")
	if err != nil {
		return erc20Token{}, fmt.Errorf("decimals: %w", err)
	}
	dec, ok := decimals[0].(uint8)
	if !ok {
		return erc20Token{}, fmt.Errorf("decimals: unexpected return %T", decimals[0])
	}
	symbol, err := r.erc20Call(ctx, from, addr, "symbol")
	if err != nil {
		return erc20Token{}, fmt.Errorf("symbol: %w", err)
	}
	sym, ok := symbol[0].(string)
	if !ok {
		return erc20Token{}, fmt.Errorf("symbol: unexpected return %T", symbol[0])
	}
	return erc20Token{address: addr, symbol: sym, scale: math.Pow10(int(dec))}, nil
}

func (r *Reporter) erc20Call(ctx sdk.Context, from sdk.AccAddress, to common.Address, method string, args ...interface{}) ([]interface{}, error) {
	data, err := erc20ABI.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	ret, err := r.keepers.EVM.StaticCallEVM(ctx, from, &to, data)
	if err != nil {
		return nil, err
	}
	out, err := erc20ABI.Unpack(method, ret)
	if err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, fmt.Errorf("%s: %d return values", method, len(out))
	}
	return out, nil
}
