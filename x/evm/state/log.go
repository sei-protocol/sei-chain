package state

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type Logs struct {
	Ls []*ethtypes.Log `json:"logs"`
}

func (s *DBImpl) AddLog(l *ethtypes.Log) {
	s.logs = append(s.logs, l)
}

func (s *DBImpl) GetAllLogs() []*ethtypes.Log {
	res := []*ethtypes.Log{}
	for _, logs := range s.snapshottedLogs {
		res = append(res, logs...)
	}
	res = append(res, s.logs...)
	return res
}

func (s *DBImpl) GetLogs(common.Hash, uint64, common.Hash) []*ethtypes.Log {
	return s.GetAllLogs()
}
