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
	s.tempStateCurrent.logs = append(s.tempStateCurrent.logs, l)

	if s.logger != nil && s.logger.OnLog != nil {
		s.logger.OnLog(l)
	}
}

func (s *DBImpl) GetAllLogs() []*ethtypes.Log {
	res := []*ethtypes.Log{}
	for _, st := range s.tempStatesHist {
		res = append(res, st.logs...)
	}
	res = append(res, s.tempStateCurrent.logs...)
	return res
}

func (s *DBImpl) GetLogs(common.Hash, uint64, common.Hash) []*ethtypes.Log {
	return s.GetAllLogs()
}

func (s *DBImpl) Logs() []*ethtypes.Log {
	return s.GetAllLogs()
}
