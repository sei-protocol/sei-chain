package cosmosmetrics

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"go.opentelemetry.io/otel/attribute"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// EVMKeeper is the EVM state the reporter reads.
type EVMKeeper interface {
	GetEVMAddressOrDefault(sdk.Context, sdk.AccAddress) common.Address
	StaticCallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, data []byte) ([]byte, error)
}

// erc20CallGasLimit bounds each ERC-20 static call, the same ceiling the EVM gRPC querier applies
// to StaticCall by default. A configured token that loops burns this much and is skipped, not the
// refresh.
const erc20CallGasLimit uint64 = 300_000

// erc20SymbolMaxLen bounds the symbol label; a contract controls the string it returns.
const erc20SymbolMaxLen = 32

// errERC20OutOfGas is returned by staticCall when the contract ran the call's gas out.
var errERC20OutOfGas = errors.New("out of gas")

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
	seen := make(map[common.Address]struct{}, len(addrs))
	for _, a := range addrs {
		if !common.IsHexAddress(a) {
			return nil, fmt.Errorf("%s: %q: not a 0x address", flagERC20Tokens, a)
		}
		addr := common.HexToAddress(a)
		if _, dup := seen[addr]; dup {
			return nil, fmt.Errorf("%s: %q: listed more than once", flagERC20Tokens, a)
		}
		seen[addr] = struct{}{}
		tokens = append(tokens, addr)
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
	tokens := make(map[common.Address]erc20Token, len(r.erc20Tokens))
	for _, addr := range r.erc20Tokens {
		token, err := r.erc20Token(ctx, addr)
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("erc20 %s: %w", addr, err))
			continue
		}
		tokens[addr] = token
	}
	for _, acc := range r.wallets {
		evmAddr := r.keepers.EVM.GetEVMAddressOrDefault(ctx, acc)
		for _, addr := range r.erc20Tokens {
			identity := make([]attribute.KeyValue, 2, 4)
			identity[0] = addressAttr(acc.String())
			identity[1] = attribute.String("token", addr.Hex())
			token, ok := tokens[addr]
			if !ok {
				b.gauge(cosmosMetrics.walletERC20ReadOK, 0, identity...)
				continue
			}
			amount, err := r.erc20BalanceOf(ctx, acc, addr, evmAddr)
			if err != nil {
				b.errs = append(b.errs, fmt.Errorf("wallet %s: erc20 %s balanceOf: %w", acc, addr, err))
				b.gauge(cosmosMetrics.walletERC20ReadOK, 0, identity...)
				if errors.Is(err, errERC20OutOfGas) {
					// A balanceOf that loops for one wallet loops for all of them; do not pay for it again this refresh.
					delete(tokens, addr)
				}
				continue
			}
			b.gauge(cosmosMetrics.walletERC20ReadOK, 1, identity...)
			b.int(cosmosMetrics.walletERC20Balance, sdk.NewIntFromBigInt(amount), token.scale,
				append(identity, attribute.String("evm_address", evmAddr.Hex()), attribute.String("symbol", token.symbol))...)
		}
	}
}

func (r *Reporter) erc20BalanceOf(ctx sdk.Context, acc sdk.AccAddress, token, evmAddr common.Address) (*big.Int, error) {
	balance, err := r.erc20Call(ctx, acc, token, "balanceOf", evmAddr)
	if err != nil {
		return nil, err
	}
	amount, ok := balance[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected return %T", balance[0])
	}
	return amount, nil
}

// erc20Token returns a token's symbol and scale. The scale is read from the contract on every
// refresh so a token that changes its decimals is reported in its current units. The symbol is a
// series label, so it is read once and then fixed for the life of the reporter and the series'
// identity never changes underneath the alerts keyed on it.
func (r *Reporter) erc20Token(ctx sdk.Context, addr common.Address) (erc20Token, error) {
	from := r.wallets[0]
	decimals, err := r.erc20Call(ctx, from, addr, "decimals")
	if err != nil {
		return erc20Token{}, fmt.Errorf("decimals: %w", err)
	}
	dec, ok := decimals[0].(uint8)
	if !ok {
		return erc20Token{}, fmt.Errorf("decimals: unexpected return %T", decimals[0])
	}
	symbol, ok := r.erc20Symbols[addr]
	if !ok {
		symbol = r.readERC20Symbol(ctx, from, addr)
		r.erc20Symbols[addr] = symbol
	}
	return erc20Token{address: addr, symbol: symbol, scale: math.Pow10(int(dec))}, nil
}

// readERC20Symbol returns the token's symbol, or "" when the contract has no symbol() or returns
// one that is not an ABI string. The symbol is only a label, so its absence does not stop the
// balance from being reported.
func (r *Reporter) readERC20Symbol(ctx sdk.Context, from sdk.AccAddress, addr common.Address) string {
	symbol, err := r.erc20Call(ctx, from, addr, "symbol")
	if err != nil {
		return ""
	}
	sym, ok := symbol[0].(string)
	if !ok {
		return ""
	}
	return sanitizeERC20Symbol(sym)
}

// sanitizeERC20Symbol keeps the printable, non-space characters of a contract-supplied symbol,
// at most erc20SymbolMaxLen of them.
func sanitizeERC20Symbol(s string) string {
	var b strings.Builder
	n := 0
	for _, c := range s {
		if !unicode.IsPrint(c) || unicode.IsSpace(c) {
			continue
		}
		if n == erc20SymbolMaxLen {
			break
		}
		b.WriteRune(c)
		n++
	}
	return b.String()
}

func (r *Reporter) erc20Call(ctx sdk.Context, from sdk.AccAddress, to common.Address, method string, args ...interface{}) ([]interface{}, error) {
	data, err := erc20ABI.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	ret, err := r.staticCall(ctx, from, to, data)
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

// staticCall runs one EVM static call under a finite gas meter. Running out of gas, whether the EVM
// reports it as an error or the Sei gas meter reports it by panicking, is returned as
// errERC20OutOfGas; any other panic is returned as an error too.
func (r *Reporter) staticCall(ctx sdk.Context, from sdk.AccAddress, to common.Address, data []byte) (ret []byte, err error) {
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, erc20CallGasLimit))
	defer func() {
		switch p := recover().(type) {
		case nil:
		case sdk.ErrorOutOfGas:
			ret, err = nil, fmt.Errorf("%w: %s", errERC20OutOfGas, p.Descriptor)
		default:
			ret, err = nil, fmt.Errorf("static call panicked: %v", p)
		}
	}()
	ret, err = r.keepers.EVM.StaticCallEVM(ctx, from, &to, data)
	if errors.Is(err, vm.ErrOutOfGas) {
		err = fmt.Errorf("%w: %v", errERC20OutOfGas, err)
	}
	return ret, err
}
