package multiversion

import (
	"sync"

	"github.com/cosmos/cosmos-sdk/store/types"
)

type MultiVersionValue interface {
	GetLatest() (value MultiVersionValueItem, found bool)
	GetLatestNonEstimate() (value MultiVersionValueItem, found bool)
	GetLatestBeforeIndex(index int) (value MultiVersionValueItem, found bool)
	Set(index int, incarnation int, value []byte)
	SetEstimate(index int, incarnation int)
	Delete(index int, incarnation int)
	Remove(index int)
}

type MultiVersionValueItem interface {
	IsDeleted() bool
	IsEstimate() bool
	Value() []byte
	Incarnation() int
	Index() int
}

// multiVersionItem stores versioned values for a single key as a sorted
// slice instead of a btree. Most keys are touched by only 1-3 transactions,
// making a flat slice faster and far cheaper to allocate than a btree with
// its FreeList and internal nodes (~392 bytes overhead per btree).
type multiVersionItem struct {
	items []*valueItem // sorted by index ascending
	mtx   sync.RWMutex
}

var _ MultiVersionValue = (*multiVersionItem)(nil)

func NewMultiVersionItem() *multiVersionItem {
	return &multiVersionItem{
		items: make([]*valueItem, 0, 2),
	}
}

// GetLatest returns the latest written value (highest index).
func (item *multiVersionItem) GetLatest() (MultiVersionValueItem, bool) {
	item.mtx.RLock()
	defer item.mtx.RUnlock()

	if len(item.items) == 0 {
		return nil, false
	}
	return item.items[len(item.items)-1], true
}

// GetLatestNonEstimate returns the latest written value that isn't an ESTIMATE.
func (item *multiVersionItem) GetLatestNonEstimate() (MultiVersionValueItem, bool) {
	item.mtx.RLock()
	defer item.mtx.RUnlock()

	for i := len(item.items) - 1; i >= 0; i-- {
		if !item.items[i].estimate {
			return item.items[i], true
		}
	}
	return nil, false
}

// GetLatestBeforeIndex returns the latest value with index strictly less than
// the given index. No temporary pivot object is allocated (unlike the btree
// DescendLessOrEqual approach which allocated 34M pivot objects).
func (item *multiVersionItem) GetLatestBeforeIndex(index int) (MultiVersionValueItem, bool) {
	item.mtx.RLock()
	defer item.mtx.RUnlock()

	for i := len(item.items) - 1; i >= 0; i-- {
		if item.items[i].index < index {
			return item.items[i], true
		}
	}
	return nil, false
}

func (item *multiVersionItem) Set(index int, incarnation int, value []byte) {
	types.AssertValidValue(value)
	item.mtx.Lock()
	defer item.mtx.Unlock()

	item.replaceOrInsert(&valueItem{
		index:       index,
		incarnation: incarnation,
		value:       value,
	})
}

func (item *multiVersionItem) Delete(index int, incarnation int) {
	item.mtx.Lock()
	defer item.mtx.Unlock()

	item.replaceOrInsert(&valueItem{
		index:       index,
		incarnation: incarnation,
	})
}

func (item *multiVersionItem) Remove(index int) {
	item.mtx.Lock()
	defer item.mtx.Unlock()

	for i, v := range item.items {
		if v.index == index {
			item.items = append(item.items[:i], item.items[i+1:]...)
			return
		}
	}
}

func (item *multiVersionItem) SetEstimate(index int, incarnation int) {
	item.mtx.Lock()
	defer item.mtx.Unlock()

	item.replaceOrInsert(&valueItem{
		index:       index,
		incarnation: incarnation,
		estimate:    true,
	})
}

// replaceOrInsert inserts vi into the sorted slice, replacing any existing
// item with the same index.
func (item *multiVersionItem) replaceOrInsert(vi *valueItem) {
	n := len(item.items)
	// Fast path: append if vi has the highest index (common case when
	// transactions are processed in order).
	if n == 0 || item.items[n-1].index < vi.index {
		item.items = append(item.items, vi)
		return
	}
	// Linear scan for the correct position.
	for i := 0; i < n; i++ {
		if item.items[i].index == vi.index {
			item.items[i] = vi
			return
		}
		if item.items[i].index > vi.index {
			item.items = append(item.items, nil)
			copy(item.items[i+1:], item.items[i:])
			item.items[i] = vi
			return
		}
	}
}

type valueItem struct {
	index       int
	incarnation int
	value       []byte
	estimate    bool
}

var _ MultiVersionValueItem = (*valueItem)(nil)

func (v *valueItem) Index() int {
	return v.index
}

func (v *valueItem) Incarnation() int {
	return v.incarnation
}

func (v *valueItem) IsDeleted() bool {
	return v.value == nil && !v.estimate
}

func (v *valueItem) IsEstimate() bool {
	return v.estimate
}

func (v *valueItem) Value() []byte {
	return v.value
}

func NewValueItem(index int, incarnation int, value []byte) *valueItem {
	return &valueItem{
		index:       index,
		incarnation: incarnation,
		value:       value,
	}
}

func NewEstimateItem(index int, incarnation int) *valueItem {
	return &valueItem{
		index:       index,
		incarnation: incarnation,
		estimate:    true,
	}
}

func NewDeletedItem(index int, incarnation int) *valueItem {
	return &valueItem{
		index:       index,
		incarnation: incarnation,
	}
}
