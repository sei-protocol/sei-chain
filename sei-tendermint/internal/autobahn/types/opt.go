package types

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// NextOpt defaults to 0.
func NextOpt[I ~uint64, T interface{ Next() I }](mv utils.Option[T]) I {
	if v, ok := mv.Get(); ok {
		return v.Next()
	}
	return 0
}

// ProposalOpt extracts optional proposal from optional value.
func ProposalOpt[P any, T interface{ Proposal() P }](mv utils.Option[T]) utils.Option[P] {
	if v, ok := mv.Get(); ok {
		return utils.Some(v.Proposal())
	}
	return utils.None[P]()
}

// NextIndexOpt defaults to 0.
func NextIndexOpt[T interface{ Index() RoadIndex }](mv utils.Option[T]) RoadIndex {
	if v, ok := mv.Get(); ok {
		return v.Index() + 1
	}
	return 0
}

// NextViewOpt defaults to {0,0}.
func NextViewOpt[T interface{ View() View }](mv utils.Option[T]) View {
	if v, ok := mv.Get(); ok {
		return v.View().Next()
	}
	return View{Index: 0, Number: 0}
}

// LaneRangeOpt defaults to an empty initial range.
func LaneRangeOpt[T interface {
	LaneRange(lane LaneID) *LaneRange
}](mv utils.Option[T], lane LaneID) *LaneRange {
	if v, ok := mv.Get(); ok {
		return v.LaneRange(lane)
	}
	return NewLaneRange(lane, 0, utils.None[*BlockHeader]())
}

// GlobalRangeOpt defaults to an empty initial range.
func GlobalRangeOpt[T interface{ GlobalRange() GlobalRange }](mv utils.Option[T]) GlobalRange {
	if v, ok := mv.Get(); ok {
		return v.GlobalRange()
	}
	return GlobalRange{}
}

// AppOpt defaults to None.
func AppOpt[T interface {
	App() utils.Option[*AppProposal]
}](mv utils.Option[T]) utils.Option[*AppProposal] {
	if v, ok := mv.Get(); ok {
		return v.App()
	}
	return utils.None[*AppProposal]()
}
