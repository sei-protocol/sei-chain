package types

import "errors"

// ErrNotFound indicates that requested data is not yet available.
var ErrNotFound = errors.New("not found")

// ErrUnexpectedIndex indicates a repeated, skipped, stale, or unrepresentable write index.
var ErrUnexpectedIndex = errors.New("unexpected road index")

// ErrMissingQC indicates that an application proposal has no stored CommitQC.
var ErrMissingQC = errors.New("missing CommitQC")

// ErrMissingAppProposal indicates that an AppQC has no stored application proposal.
var ErrMissingAppProposal = errors.New("missing AppProposal")

// ErrPruned indicates that requested data is below the current retention floor.
// The floor is re-derived on reopen, so hidden records may become readable again.
var ErrPruned = errors.New("pruned: below retention watermark")

// ErrQCBlocksMismatch indicates inconsistent SuperBlock metadata, hash, or lane blocks.
var ErrQCBlocksMismatch = errors.New("QC blocks do not match headers")
