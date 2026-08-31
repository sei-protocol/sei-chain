package types

import (
	"context"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type slashDelegationModificationKey struct{}

// WithSlashDelegationModification marks delegation changes made while applying a validator slash.
func WithSlashDelegationModification(ctx sdk.Context) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), slashDelegationModificationKey{}, true))
}

// IsSlashDelegationModification reports whether a delegation change is part of a validator slash.
func IsSlashDelegationModification(ctx sdk.Context) bool {
	marked, _ := ctx.Context().Value(slashDelegationModificationKey{}).(bool)
	return marked
}
