package evmrpc

import (
	"strings"
)

type QueryBuilder struct {
	conditions []string
}

func NewHeadQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		conditions: []string{
			"tm.event = 'NewBlockHeader'",
		},
	}
}

func (q *QueryBuilder) Build() string {
	return strings.Join(q.conditions, " AND ")
}
