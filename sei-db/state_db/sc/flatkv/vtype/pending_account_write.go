package vtype

// PendingAccountWrite tracks field-level changes to an account that have not yet been committed.
// Each field has a value and a flag indicating whether it has been set. Only set fields are
// applied when merging into a base AccountData.
//
// A PendingAccountWrite should only be created when there is at least one change to record.
type PendingAccountWrite struct {
	balance  *[32]byte
	nonce    uint64
	nonceSet bool
	codeHash *[32]byte
}

// NewPendingAccountWrite creates a new PendingAccountWrite with no fields set.
func NewPendingAccountWrite() *PendingAccountWrite {
	return &PendingAccountWrite{}
}

// GetBalance returns the pending balance value, or nil if not set.
func (p *PendingAccountWrite) GetBalance() *[32]byte { return p.balance }

// IsBalanceSet reports whether the balance has been set in this pending write.
func (p *PendingAccountWrite) IsBalanceSet() bool { return p.balance != nil }

// GetNonce returns the pending nonce value.
func (p *PendingAccountWrite) GetNonce() uint64 { return p.nonce }

// IsNonceSet reports whether the nonce has been set in this pending write.
func (p *PendingAccountWrite) IsNonceSet() bool { return p.nonceSet }

// GetCodeHash returns the pending code hash value, or nil if not set.
func (p *PendingAccountWrite) GetCodeHash() *[32]byte { return p.codeHash }

// IsCodeHashSet reports whether the code hash has been set in this pending write.
func (p *PendingAccountWrite) IsCodeHashSet() bool { return p.codeHash != nil }

// SetBalance marks the balance as changed. The pointer is stored directly; the caller
// must not modify the underlying array after calling SetBalance. Returns self.
func (p *PendingAccountWrite) SetBalance(balance *[32]byte) *PendingAccountWrite {
	p.balance = balance
	return p
}

// SetNonce marks the nonce as changed. Returns self.
func (p *PendingAccountWrite) SetNonce(nonce uint64) *PendingAccountWrite {
	p.nonce = nonce
	p.nonceSet = true
	return p
}

// SetCodeHash marks the code hash as changed. The pointer is stored directly; the caller
// must not modify the underlying array after calling SetCodeHash. Returns self.
func (p *PendingAccountWrite) SetCodeHash(codeHash *[32]byte) *PendingAccountWrite {
	p.codeHash = codeHash
	return p
}

// Merge applies the pending field changes onto a copy of the base AccountData, updating the
// block height. Only fields that have been set via Set* methods are overwritten; all other
// fields are carried over from the base. The base is not modified.
func (p *PendingAccountWrite) Merge(base *AccountData, blockHeight int64) *AccountData {
	result := base.Copy().SetBlockHeight(blockHeight)

	if p.balance != nil {
		result.SetBalance(p.balance)
	}
	if p.nonceSet {
		result.SetNonce(p.nonce)
	}
	if p.codeHash != nil {
		result.SetCodeHash(p.codeHash)
	}

	return result
}
