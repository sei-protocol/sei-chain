package vtype

// PendingAccountWrite tracks field-level changes to an account that have not yet been committed.
// Each field has a value and a flag indicating whether it has been set. Only set fields are
// applied when merging into a base AccountData.
//
// It is legal to operate on a nil PendingAccountWrite. A nil PendingAccountWrite will always return 0s from getters,
// and will return a non-nil result when a setter is called.
type PendingAccountWrite struct {
	balance  *Balance
	nonce    uint64
	nonceSet bool
	codeHash *CodeHash
}

// NewPendingAccountWrite creates a new PendingAccountWrite with no fields set.
func NewPendingAccountWrite() *PendingAccountWrite {
	return &PendingAccountWrite{}
}

// GetBalance returns the pending balance value, or nil if not set.
func (p *PendingAccountWrite) GetBalance() *Balance {
	if p == nil {
		zero := Balance{}
		return &zero
	}
	return p.balance
}

// IsBalanceSet reports whether the balance has been set in this pending write.
func (p *PendingAccountWrite) IsBalanceSet() bool {
	if p == nil {
		return false
	}
	return p.balance != nil
}

// GetNonce returns the pending nonce value.
func (p *PendingAccountWrite) GetNonce() uint64 {
	if p == nil {
		return 0
	}
	return p.nonce
}

// IsNonceSet reports whether the nonce has been set in this pending write.
func (p *PendingAccountWrite) IsNonceSet() bool {
	if p == nil {
		return false
	}
	return p.nonceSet
}

// GetCodeHash returns the pending code hash value, or nil if not set.
func (p *PendingAccountWrite) GetCodeHash() *CodeHash {
	if p == nil {
		zero := CodeHash{}
		return &zero
	}
	return p.codeHash
}

// IsCodeHashSet reports whether the code hash has been set in this pending write.
func (p *PendingAccountWrite) IsCodeHashSet() bool {
	if p == nil {
		return false
	}
	return p.codeHash != nil
}

// SetBalance marks the balance as changed. The pointer is stored directly; the caller
// must not modify the underlying array after calling SetBalance. Returns self.
func (p *PendingAccountWrite) SetBalance(balance *Balance) *PendingAccountWrite {
	if p == nil {
		p = NewPendingAccountWrite()
	}
	p.balance = balance
	return p
}

// SetNonce marks the nonce as changed. Returns self.
func (p *PendingAccountWrite) SetNonce(nonce uint64) *PendingAccountWrite {
	if p == nil {
		p = NewPendingAccountWrite()
	}
	p.nonce = nonce
	p.nonceSet = true
	return p
}

// SetCodeHash marks the code hash as changed. The pointer is stored directly; the caller
// must not modify the underlying array after calling SetCodeHash. Returns self.
func (p *PendingAccountWrite) SetCodeHash(codeHash *CodeHash) *PendingAccountWrite {
	if p == nil {
		p = NewPendingAccountWrite()
	}
	p.codeHash = codeHash
	return p
}

// Merge applies the pending field changes onto a copy of the base AccountData, updating the
// block height. Only fields that have been set via Set* methods are overwritten; all other
// fields are carried over from the base. The base is not modified. If a nil base is provided,
// the pending writes are applied to a new AccountData instantiated to all 0s.
func (p *PendingAccountWrite) Merge(base *AccountData, blockHeight int64) *AccountData {
	var result *AccountData
	if base == nil {
		result = NewAccountData()
	} else {
		result = base.Copy()
	}

	result.SetBlockHeight(blockHeight)

	if p != nil {
		if p.balance != nil {
			result.SetBalance(p.balance)
		}
		if p.nonceSet {
			result.SetNonce(p.nonce)
		}
		if p.codeHash != nil {
			result.SetCodeHash(p.codeHash)
		}
	}

	return result
}
