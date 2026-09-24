package receipt

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/stretchr/testify/require"
)

func TestMatchLog(t *testing.T) {
	t.Parallel()
	addrA := common.HexToAddress("0xa")
	addrB := common.HexToAddress("0xb")
	topicX := common.HexToHash("0x1")
	topicY := common.HexToHash("0x2")
	lg := &ethtypes.Log{Address: addrA, Topics: []common.Hash{topicX}}

	tests := []struct {
		name string
		crit filters.FilterCriteria
		want bool
	}{
		{name: "empty criteria", want: true},
		{name: "address match", crit: filters.FilterCriteria{Addresses: []common.Address{addrB, addrA}}, want: true},
		{name: "address mismatch", crit: filters.FilterCriteria{Addresses: []common.Address{addrB}}, want: false},
		{name: "topic match", crit: filters.FilterCriteria{Topics: [][]common.Hash{{topicX}}}, want: true},
		{name: "topic OR match", crit: filters.FilterCriteria{Topics: [][]common.Hash{{topicY, topicX}}}, want: true},
		{name: "topic mismatch", crit: filters.FilterCriteria{Topics: [][]common.Hash{{topicY}}}, want: false},
		{name: "wildcard position", crit: filters.FilterCriteria{Topics: [][]common.Hash{nil}}, want: true},
		{name: "constrained position beyond log", crit: filters.FilterCriteria{Topics: [][]common.Hash{{topicX}, {topicY}}}, want: false},
		{name: "trailing wildcard beyond log is lenient", crit: filters.FilterCriteria{Topics: [][]common.Hash{{topicX}, nil}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, MatchLog(lg, tt.crit))
		})
	}
}
