package receipt

import controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"

var _ controllertypes.PrunableStore = (*receiptStore)(nil)

func (s *receiptStore) Name() string {
	return "ReceiptDB"
}

// ExternalPruning is always false: this backend prunes itself on KeepRecent and
// refuses to open with ExternalPruning set.
func (s *receiptStore) ExternalPruning() bool {
	return false
}

func (s *receiptStore) PruneHistory(uint64) error { return nil }

func (s *receiptStore) PruneSnapshots(uint64) error { return nil }

func (s *receiptStore) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

func (s *receiptStore) GetLatestBlock() (uint64, error) {
	return s.LatestVersion(), nil
}
