package types

import ethtypes "github.com/ethereum/go-ethereum/core/types"

func NewLogsFromEth(ethlogs []*ethtypes.Log) []*Log {
	logs := make([]*Log, 0, len(ethlogs))
	for _, ethlog := range ethlogs {
		logs = append(logs, newLogFromEth(ethlog))
	}
	return logs
}

func newLogFromEth(log *ethtypes.Log) *Log {
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.String()
	}

	return &Log{
		Address: log.Address.String(),
		Topics:  topics,
		Data:    log.Data,
		Index:   uint32(log.Index),
	}
}
