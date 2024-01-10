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

func NewBlockQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		conditions: []string{
			"tm.event = 'NewBlock'",
		},
	}
}

func (q *QueryBuilder) Build() string {
	return strings.Join(q.conditions, " AND ")
}
