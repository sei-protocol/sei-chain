package state

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type Logs struct {
	Ls []*ethtypes.Log `json:"logs"`
}

func (s *DBImpl) AddLog(l *ethtypes.Log) {
	l.Index = uint(len(s.GetAllLogs()))
	s.tempState.logs = append(s.tempState.logs, l)
	s.journal = append(s.journal, &addLogChange{})

	if s.logger != nil && s.logger.OnLog != nil {
		s.logger.OnLog(l)
	}
}

func (s *DBImpl) GetAllLogs() []*ethtypes.Log {
	res := []*ethtypes.Log{}
	res = append(res, s.tempState.logs...)
	return res
}

func (s *DBImpl) GetLogs(common.Hash, uint64, common.Hash) []*ethtypes.Log {
	return s.GetAllLogs()
}

func (s *DBImpl) Logs() []*ethtypes.Log {
	return s.GetAllLogs()
}
