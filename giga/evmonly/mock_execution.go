package evmonly

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// mockERC20BalancesSlot is the storage slot of the _balances mapping in sei-load's ERC20 contract,
// after name, symbol, decimals and totalSupply.
const mockERC20BalancesSlot = 4

var (
	mockERC20TransferSelector = crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
	mockERC20TransferTopic    = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
)

// needsRealExecution reports whether a block holds a transaction the mock cannot model: a contract
// creation or a code delegation, both of which write code.
func needsRealExecution(req PreparedBlock) bool {
	for _, p := range req.Txs {
		if p.Tx.To() == nil || p.Tx.Type() == ethtypes.SetCodeTxType {
			return true
		}
	}
	return false
}

// executeBlockMock builds a block's result without running the EVM. Every transaction pays its gas
// limit at its effective price, bumps its sender's nonce and moves its value; an ERC20
// transfer(address,uint256) also applies sei-load's ERC20 balance logic to the token's storage.
// State is read from source and the result has the shape the real executor produces.
func (e *Executor) executeBlockMock(ctx context.Context, req PreparedBlock, source StateReader) (*BlockResult, error) {
	result, err := e.acquireBlockResult(ctx, len(req.Txs))
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			result.Release()
		}
	}()
	state := newMockBlockState(source)
	gasLeft := req.Context.GasLimit
	baseFee := cloneOptionalBig(req.Context.BaseFee)
	for txIndex, p := range req.Txs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		txResult, receipt, err := e.mockTx(state, &gasLeft, req.Context, p, uint(txIndex), baseFee) //nolint:gosec // G115: txIndex is a non-negative slice index.
		if err != nil {
			return nil, fmt.Errorf("mock tx %d %s: %w", txIndex, p.Tx.Hash(), err)
		}
		txResult.CumulativeGasUsed = result.GasUsed + txResult.GasUsed
		receipt.CumulativeGasUsed = txResult.CumulativeGasUsed
		result.Txs = append(result.Txs, txResult)
		result.Receipts = append(result.Receipts, receipt)
		result.GasUsed += txResult.GasUsed
	}
	state.changeSetInto(&result.ChangeSet)
	ok = true
	return result, nil
}

// mockTx applies one transaction to state. It refuses what the real executor refuses, and with
// RejectUnappliableTxs records a transaction it cannot apply as rejected rather than failing the block.
func (e *Executor) mockTx(
	state *mockBlockState,
	gasLeft *uint64,
	block BlockContext,
	p PreparedTx,
	txIndex uint,
	baseFee *big.Int,
) (TxResult, *ethtypes.Receipt, error) {
	tx := p.Tx
	if err := validateSupportedTx(tx); err != nil {
		return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: err}, nil, err
	}
	price := EffectiveGasPrice(tx, baseFee)
	if !e.cfg.DisableGasPriceCheck && e.cfg.MinGasPrice != nil && price.Cmp(e.cfg.MinGasPrice) < 0 {
		return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: errInsufficientGasPrice},
			nil,
			errInsufficientGasPrice
	}

	gas := tx.Gas()
	fee := new(big.Int).Mul(new(big.Int).SetUint64(gas), price)
	cost := new(big.Int).Add(fee, tx.Value())
	sender := state.account(p.Sender)
	var cause error
	switch {
	case !e.cfg.DisableNonceCheck && tx.Nonce() < sender.nonce:
		cause = fmt.Errorf("%w: address %v, tx: %d state: %d", core.ErrNonceTooLow, p.Sender, tx.Nonce(), sender.nonce)
	case !e.cfg.DisableNonceCheck && tx.Nonce() > sender.nonce:
		cause = fmt.Errorf("%w: address %v, tx: %d state: %d", core.ErrNonceTooHigh, p.Sender, tx.Nonce(), sender.nonce)
	case gas > *gasLeft:
		cause = core.ErrGasLimitReached
	case sender.balance.Cmp(cost) < 0:
		cause = fmt.Errorf("%w: address %v have %v want %v", core.ErrInsufficientFunds, p.Sender, sender.balance, cost)
	}
	if cause != nil {
		if !e.cfg.RejectUnappliableTxs {
			return TxResult{Hash: tx.Hash(), Sender: p.Sender, To: tx.To(), Err: cause}, nil, cause
		}
		txResult, receipt := rejectedTx(p, block, txIndex, baseFee, cause)
		return txResult, receipt, nil
	}

	*gasLeft -= gas
	sender.nonce++
	sender.balance.Sub(sender.balance, cost)
	recipient := state.account(*tx.To())
	recipient.balance.Add(recipient.balance, tx.Value())
	tip := new(big.Int).Set(price)
	if baseFee != nil {
		tip.Sub(tip, baseFee)
	}
	coinbase := state.account(block.Coinbase)
	coinbase.balance.Add(coinbase.balance, tip.Mul(tip, new(big.Int).SetUint64(gas)))

	var logs []*ethtypes.Log
	if log, ok := state.applyERC20Transfer(p.Sender, *tx.To(), tx.Data()); ok {
		log.BlockNumber = block.Number
		log.BlockHash = block.BlockHash
		log.TxHash = tx.Hash()
		log.TxIndex = txIndex
		logs = []*ethtypes.Log{log}
	}

	receipt := &ethtypes.Receipt{
		Type:              tx.Type(),
		Status:            ethtypes.ReceiptStatusSuccessful,
		Logs:              logs,
		TxHash:            tx.Hash(),
		GasUsed:           gas,
		EffectiveGasPrice: price,
		BlockHash:         block.BlockHash,
		BlockNumber:       new(big.Int).SetUint64(block.Number),
		TransactionIndex:  txIndex,
	}
	receipt.Bloom = ethtypes.CreateBloom(receipt)
	return TxResult{
		Hash:              tx.Hash(),
		Sender:            p.Sender,
		To:                tx.To(),
		Status:            ethtypes.ReceiptStatusSuccessful,
		GasUsed:           gas,
		EffectiveGasPrice: new(big.Int).Set(price),
		Logs:              logs,
	}, receipt, nil
}

// mockAccount is an account as the block has left it, beside the value the block first read.
type mockAccount struct {
	nonce, priorNonce     uint64
	balance, priorBalance *big.Int
}

// mockSlot is a storage slot as the block has left it, beside the value the block first read.
type mockSlot struct {
	value, prior common.Hash
}

// mockBlockState is the state a mocked block reads and writes, loaded from source on first touch.
type mockBlockState struct {
	source   StateReader
	accounts map[common.Address]*mockAccount
	storage  map[common.Address]map[common.Hash]*mockSlot
	logIndex uint
}

func newMockBlockState(source StateReader) *mockBlockState {
	return &mockBlockState{
		source:   source,
		accounts: map[common.Address]*mockAccount{},
		storage:  map[common.Address]map[common.Hash]*mockSlot{},
	}
}

func (s *mockBlockState) account(addr common.Address) *mockAccount {
	if acct, ok := s.accounts[addr]; ok {
		return acct
	}
	nonce := s.source.GetNonce(addr)
	balance := new(big.Int)
	if b := s.source.GetBalance(addr); b != nil {
		balance.Set(b)
	}
	acct := &mockAccount{nonce: nonce, priorNonce: nonce, balance: balance, priorBalance: new(big.Int).Set(balance)}
	s.accounts[addr] = acct
	return acct
}

func (s *mockBlockState) slot(addr common.Address, key common.Hash) *mockSlot {
	slots, ok := s.storage[addr]
	if !ok {
		slots = map[common.Hash]*mockSlot{}
		s.storage[addr] = slots
	}
	if slot, ok := slots[key]; ok {
		return slot
	}
	value := s.source.GetState(addr, key)
	slot := &mockSlot{value: value, prior: value}
	slots[key] = slot
	return slot
}

// applyERC20Transfer applies sei-load's ERC20 transfer to token's storage when data is a
// transfer(address,uint256) call, and returns the Transfer event it emits.
func (s *mockBlockState) applyERC20Transfer(from, token common.Address, data []byte) (*ethtypes.Log, bool) {
	if len(data) != 4+32+32 || !bytes.Equal(data[:4], mockERC20TransferSelector) {
		return nil, false
	}
	to := common.BytesToAddress(data[4:36])
	amount := new(uint256.Int).SetBytes32(data[36:68])

	fromSlot := s.slot(token, mockERC20BalanceSlot(from))
	fromBalance := new(uint256.Int).SetBytes32(fromSlot.value[:])
	if fromBalance.Lt(amount) {
		fromBalance.Add(fromBalance, amount)
	} else {
		fromBalance.Sub(fromBalance, amount)
	}
	fromSlot.value = fromBalance.Bytes32()

	toSlot := s.slot(token, mockERC20BalanceSlot(to))
	toBalance := new(uint256.Int).SetBytes32(toSlot.value[:])
	toSlot.value = toBalance.Add(toBalance, amount).Bytes32()

	amountWord := amount.Bytes32()
	log := &ethtypes.Log{
		Address: token,
		Topics:  []common.Hash{mockERC20TransferTopic, common.BytesToHash(from[:]), common.BytesToHash(to[:])},
		Data:    amountWord[:],
		Index:   s.logIndex,
	}
	s.logIndex++
	return log, true
}

// mockERC20BalanceSlot is the storage key of owner's entry in sei-load's ERC20 _balances mapping.
func mockERC20BalanceSlot(owner common.Address) common.Hash {
	var preimage [64]byte
	copy(preimage[12:32], owner[:])
	preimage[63] = mockERC20BalancesSlot
	return crypto.Keccak256Hash(preimage[:])
}

// changeSetInto writes every value the block changed, in address and key order, beside its prior.
func (s *mockBlockState) changeSetInto(changes *StateChangeSet) {
	changes.Reset()
	seen := make(map[common.Address]struct{}, len(s.accounts)+len(s.storage))
	addresses := make([]common.Address, 0, len(s.accounts)+len(s.storage))
	for addr := range s.accounts {
		seen[addr] = struct{}{}
		addresses = append(addresses, addr)
	}
	for addr := range s.storage {
		if _, ok := seen[addr]; !ok {
			addresses = append(addresses, addr)
		}
	}
	sort.Slice(addresses, func(i, j int) bool {
		return bytes.Compare(addresses[i][:], addresses[j][:]) < 0
	})
	for _, addr := range addresses {
		if acct, ok := s.accounts[addr]; ok {
			if acct.balance.Cmp(acct.priorBalance) != 0 {
				changes.Balances = append(changes.Balances, BalanceChange{
					Address:      addr,
					Balance:      new(big.Int).Set(acct.balance),
					PriorBalance: new(big.Int).Set(acct.priorBalance),
				})
			}
			if acct.nonce != acct.priorNonce {
				changes.Nonces = append(changes.Nonces, NonceChange{
					Address:    addr,
					Nonce:      acct.nonce,
					PriorNonce: acct.priorNonce,
				})
			}
		}
		slots := s.storage[addr]
		keys := make([]common.Hash, 0, len(slots))
		for key, slot := range slots {
			if slot.value != slot.prior {
				keys = append(keys, key)
			}
		}
		sort.Slice(keys, func(i, j int) bool {
			return bytes.Compare(keys[i][:], keys[j][:]) < 0
		})
		for _, key := range keys {
			slot := slots[key]
			changes.Storage = append(changes.Storage, StorageChange{
				Address:    addr,
				Key:        key,
				Value:      slot.value,
				Delete:     slot.value == (common.Hash{}),
				PriorValue: slot.prior,
			})
		}
	}
}
