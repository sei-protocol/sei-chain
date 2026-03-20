package types

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"iter"
	"math/big"
	"slices"
	"sort"
)

// SortedSet is an immutable set of elements.
// It supports iterating over elements in order,
// and O(1) access to elements by index.
type SortedSet[T Compare[T]] struct {
	sorted []T
	hashed map[T]struct{}
}

// Compare is an interface that defines a method for comparing two elements.
type Compare[T any] interface {
	comparable
	Compare(b T) int
}

// NewSortedSet creates a new SortedSet from a slice of elements.
func NewSortedSet[T Compare[T]](vs []T) SortedSet[T] {
	sorted := slices.Clone(vs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Compare(sorted[j]) < 0
	})
	hashed := make(map[T]struct{}, len(vs))
	for _, v := range vs {
		hashed[v] = struct{}{}
	}
	return SortedSet[T]{
		sorted: sorted,
		hashed: hashed,
	}
}

// All returns an iterator over all elements in order.
func (s SortedSet[T]) All() iter.Seq2[int, T] {
	return slices.All(s.sorted)
}

// Len returns the number of elements in the set.
func (s SortedSet[T]) Len() int {
	return len(s.sorted)
}

// Has checks if the set contains the given value.
func (s SortedSet[T]) Has(val T) bool {
	_, ok := s.hashed[val]
	return ok
}

// At returns the element at the given index.
func (s SortedSet[T]) At(i int) T {
	return s.sorted[i]
}

// Committee represents the consensus committee.
type Committee struct {
	replicas SortedSet[PublicKey]
}

// Lanes is the list of nodes which are eligible to produce blocks.
func (c *Committee) Lanes() SortedSet[LaneID] { return c.replicas }

// Replicas is the list of nodes which are eligible to participate in the consensus.
func (c *Committee) Replicas() SortedSet[PublicKey] { return c.replicas }

// Leader for the consensus round with the given index.
func (c *Committee) Leader(view View) PublicKey {
	d := binary.BigEndian.AppendUint64(nil, uint64(view.Index))
	d = binary.BigEndian.AppendUint64(d, uint64(view.Number))
	h := sha256.Sum256(d)
	x := new(big.Int).SetBytes(h[:])
	i := int(x.Mod(x, big.NewInt(int64(c.replicas.Len()))).Int64())
	return c.replicas.At(i)
}

// Faulty is the number of faulty replicas that consensus can tolerate.
func (c *Committee) Faulty() int {
	// 3f < N
	return (c.replicas.Len() - 1) / 3
}

// CommitQuorum is the size of the quorum required for CommitQC.
func (c *Committee) CommitQuorum() int {
	return c.replicas.Len() - c.Faulty()
}

// AppQuorum is the size of the quorum required for AppQC.
func (c *Committee) AppQuorum() int {
	// This needs to be in range (c.Faulty(), c.CommitQuorum()]
	return c.CommitQuorum()
}

// PrepareQuorum is the size of the quorum required for PrepareQC.
func (c *Committee) PrepareQuorum() int {
	return c.CommitQuorum()
}

// TimeoutQuorum is the size of the quorum required for TimeoutQC.
func (c *Committee) TimeoutQuorum() int {
	return c.CommitQuorum()
}

// LaneQuorum is the size of the quorum required for LaneQC.
func (c *Committee) LaneQuorum() int {
	return c.Faulty() + 1
}

// NewRoundRobinElection creates a Committee with round robin election.
func NewRoundRobinElection(replicas []PublicKey) (*Committee, error) {
	if len(replicas) == 0 {
		return nil, errors.New("replicas cannot be empty")
	}
	return &Committee{
		replicas: NewSortedSet(replicas),
	}, nil
}
