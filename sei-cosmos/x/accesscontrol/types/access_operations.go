package types

import (
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
)

type AccessOperationSet struct {
	ops map[acltypes.AccessOperation]struct{}
}

func NewEmptyAccessOperationSet() *AccessOperationSet {
	return &AccessOperationSet{ops: map[acltypes.AccessOperation]struct{}{}}
}

func NewAccessOperationSet(ops []acltypes.AccessOperation) *AccessOperationSet {
	set := NewEmptyAccessOperationSet()
	set.AddMultiple(ops)
	return set
}

func (waos *AccessOperationSet) Add(op acltypes.AccessOperation) {
	waos.ops[op] = struct{}{}
}

func (waos *AccessOperationSet) AddMultiple(ops []acltypes.AccessOperation) {
	for _, op := range ops {
		waos.Add(op)
	}
}

func (waos *AccessOperationSet) Merge(other *AccessOperationSet) {
	for op := range other.ops {
		waos.Add(op)
	}
}

func (waos *AccessOperationSet) Has(op acltypes.AccessOperation) bool {
	_, ok := waos.ops[op]
	return ok
}

func (waos *AccessOperationSet) Size() int {
	return len(waos.ops)
}

func (waos *AccessOperationSet) ToSlice() []acltypes.AccessOperation {
	res := []acltypes.AccessOperation{}
	hasCommitOp := false
	for op := range waos.ops {
		if op != *CommitAccessOp() {
			res = append(res, op)
		} else {
			hasCommitOp = true
		}
	}
	if hasCommitOp {
		res = append(res, *CommitAccessOp())
	}
	return res
}

// TEST ONLY
func (waos *AccessOperationSet) HasIdentifier(identifier string) bool {
	for op := range waos.ops {
		if op.IdentifierTemplate == identifier {
			return true
		}
	}
	return false
}

func (waos *AccessOperationSet) HasResourceType(rt acltypes.ResourceType) bool {
	for op := range waos.ops {
		if op.ResourceType == rt {
			return true
		}
	}
	return false
}
