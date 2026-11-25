package blockstore

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// Seed is the records of one road for a store to start at.
type Seed struct {
	// Block is the road block of the road.
	Block *types.RoadBlock
	// AppQC is the AppQC of the road. Its proposal is the AppProposal of the road.
	AppQC *types.AppQC
}

// seed writes each record of seed that the store does not hold, and makes them durable. It
// returns ErrUnexpectedHeight when the store holds a record of another road.
func (s *Store) seed(seed Seed) error {
	road := seed.Block.QC().Height()
	status := s.Status().Or(SuffixRange{First: road, NextBlock: road, NextAppProposal: road, NextAppQC: road})
	// NextBlock bounds NextAppProposal and NextAppQC.
	if status.First != road || status.NextBlock > road+1 {
		return fmt.Errorf("the store holds %+v, want at most the records of road %d: %w", status, road, types.ErrUnexpectedHeight)
	}
	if status.NextAppQC > road {
		return nil
	}
	if status.NextBlock == road {
		if err := s.WriteBlock(seed.Block); err != nil {
			return fmt.Errorf("s.WriteBlock(): %w", err)
		}
	}
	if status.NextAppProposal == road {
		if err := s.WriteAppProposal(seed.AppQC.Proposal()); err != nil {
			return fmt.Errorf("s.WriteAppProposal(): %w", err)
		}
	}
	if err := s.WriteAppQC(seed.AppQC); err != nil {
		return fmt.Errorf("s.WriteAppQC(): %w", err)
	}
	// The caller removes the restore files after New returns, so the seed must be durable by then.
	// A crash before this flush leaves a prefix of the three records, which the next open completes.
	if err := s.Flush(); err != nil {
		return fmt.Errorf("s.Flush(): %w", err)
	}
	logger.Info("Seeded the block store", "road", road)
	return nil
}
