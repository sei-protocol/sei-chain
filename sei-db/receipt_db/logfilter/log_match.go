package logfilter

import (
	"context"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
)

type strictTopicCountKey struct{}

// WithStrictTopicCount marks a FilterLogs request as using go-ethereum's rule
// that a log with fewer topics than the filter has positions never matches,
// even when the extra positions are wildcards. Without it FilterLogs treats a
// wildcard position beyond the log's topics as matching.
func WithStrictTopicCount(ctx context.Context) context.Context {
	return context.WithValue(ctx, strictTopicCountKey{}, true)
}

// StrictTopicCount reports whether WithStrictTopicCount was applied to ctx.
func StrictTopicCount(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	strict, _ := ctx.Value(strictTopicCountKey{}).(bool)
	return strict
}

// MatchLogForQuery is the predicate FilterLogs implementations apply to each
// candidate log before charging the budget: MatchLog, plus the strict topic
// count rule when the request context asks for it.
func MatchLogForQuery(ctx context.Context, lg *ethtypes.Log, crit filters.FilterCriteria) bool {
	if StrictTopicCount(ctx) && len(crit.Topics) > len(lg.Topics) {
		return false
	}
	return MatchLog(lg, crit)
}

// MatchLog reports whether lg satisfies crit's address and positional topic
// filters. An empty topic position matches any topic, including one the log
// does not have; callers wanting go-ethereum's stricter rule that a log with
// fewer topics than filter positions never matches must check that themselves.
func MatchLog(lg *ethtypes.Log, crit filters.FilterCriteria) bool {
	// Check address filter
	if len(crit.Addresses) > 0 {
		found := false
		for _, addr := range crit.Addresses {
			if lg.Address == addr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check topic filters
	for i, topicList := range crit.Topics {
		if len(topicList) == 0 {
			continue
		}
		if i >= len(lg.Topics) {
			return false
		}
		found := false
		for _, topic := range topicList {
			if lg.Topics[i] == topic {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
