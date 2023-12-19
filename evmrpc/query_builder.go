package evmrpc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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

func NewTxQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		conditions: []string{
			"tm.event = 'Tx'", // needed for all transaction-generated events
		},
	}
}

func NewTxSearchQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		conditions: []string{},
	}
}

func (q *QueryBuilder) Build() string {
	return strings.Join(q.conditions, " AND ")
}

func (q *QueryBuilder) FilterContractAddress(contractAddr string) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s = '%s'", types.EventTypeEVMLog, types.AttributeTypeContractAddress, contractAddr))
	return q
}

func (q *QueryBuilder) FilterTopic(topic string) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s CONTAINS '%s'", types.EventTypeEVMLog, types.AttributeTypeTopics, topic))
	return q
}

func (q *QueryBuilder) FilterTopics(topics [][]string) *QueryBuilder {
	if len(topics) == 0 {
		return q
	}
	pattern, err := GetTopicsRegex(topics)
	if err != nil {
		panic(err)
	}
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s = MATCHES '%s'", types.EventTypeEVMLog, types.AttributeTypeTopics, pattern))
	return q
}

func GetTopicsRegex(topics [][]string) (string, error) {
	if len(topics) == 0 {
		return "", errors.New("topics array must be at least length 1")
	}

	topicRegex := func(topic []string) string {
		if len(topic) == 0 {
			return ""
		}
		return fmt.Sprintf("(%s)", strings.Join(topic, "|"))
	}

	return fmt.Sprintf("\\[%s.*\\]", strings.Join(utils.Map(topics, topicRegex), "[^\\,]*,")), nil
}

func (q *QueryBuilder) FilterBlockNumber(blockNumber int64) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s = '%d'", types.EventTypeEVMLog, types.AttributeTypeBlockNumber, blockNumber))
	return q
}

func (q *QueryBuilder) FilterBlockNumberStart(blockNumber int64) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s >= '%d'", types.EventTypeEVMLog, types.AttributeTypeBlockNumber, blockNumber))
	return q
}

func (q *QueryBuilder) FilterBlockNumberEnd(blockNumber int64) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s <= '%d'", types.EventTypeEVMLog, types.AttributeTypeBlockNumber, blockNumber))
	return q
}

func (q *QueryBuilder) FilterTxIndex(txIndex int64) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s = '%d'", types.EventTypeEVMLog, types.AttributeTypeTxIndex, txIndex))
	return q
}

func (q *QueryBuilder) FilterIndex(index int64) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s = '%d'", types.EventTypeEVMLog, types.AttributeTypeIndex, index))
	return q
}

func (q *QueryBuilder) FilterBlockHash(blockHash string) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s = '%s'", types.EventTypeEVMLog, types.AttributeTypeBlockHash, blockHash))
	return q
}

func (q *QueryBuilder) FilterTxHash(txHash string) *QueryBuilder {
	q.conditions = append(q.conditions, fmt.Sprintf("%s.%s = '%s'", types.EventTypeEVMLog, types.AttributeTypeTxHash, txHash))
	return q
}
