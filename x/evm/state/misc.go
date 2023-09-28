package state

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// AddPreimage records a SHA3 preimage seen by the VM.
// AddPreimage performs a no-op since the EnablePreimageRecording flag is disabled
// on the vm.Config during state transitions. No store trie preimages are written
// to the database.
func (s *StateDBImpl) AddPreimage(_ common.Hash, _ []byte) {}

func (s *StateDBImpl) AddLog(l *ethtypes.Log) { s.logs = append(s.logs, l) }
