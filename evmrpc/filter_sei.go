package evmrpc

// import (
// 	"context"
// 	"time"

// 	ethtypes "github.com/ethereum/go-ethereum/core/types"
// 	"github.com/ethereum/go-ethereum/eth/filters"
// )

// type SeiFilterAPI struct {
// 	*FilterAPI
// }

// func NewSeiFilterAPI(f *FilterAPI) *SeiFilterAPI {
// 	f.logFetcher.includeSynthetic = true
// 	return &SeiFilterAPI{f}
// }

// func (a *SeiFilterAPI) GetLogs(
// 	ctx context.Context,
// 	crit filters.FilterCriteria,
// ) (res []*ethtypes.Log, err error) {
// 	defer recordMetrics("sei_getLogs", a.connectionType, time.Now(), err == nil)
// 	logs, _, err := a.logFetcher.GetLogsByFilters(ctx, crit, 0)
// 	return logs, err
// }
