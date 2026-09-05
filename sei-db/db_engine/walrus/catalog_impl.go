package walrus

import (
	"errors"
	"fmt"
	"sync"
)

var _ Catalog = (*catalog)(nil)

// catalog records the pods and snapshots on disk and decides when one may be deleted.
type catalog struct {
	// Guards every field below it. Held only for bookkeeping, never across a file read.
	lock sync.Mutex

	// Pods ascending by block, contiguous. The orderer upstream guarantees the contiguity.
	pods []*trackedPod

	// Snapshots ascending by block. The first is the block zero pseudo-snapshot until a real one takes over
	// as the floor.
	snapshots []*trackedSnapshot

	// The block of the snapshot backwards walks terminate at. Nothing at or above it is ever deleted.
	floorBlock uint64

	// The oldest block a query may reach. Policy raises it; it never falls.
	queryFloor uint64

	// How many queries currently hold references.
	inFlight int

	// The instance name this catalog's metrics are labeled with.
	name string
}

// trackedPod is a pod and the number of queries currently reading it.
type trackedPod struct {
	pod        *Pod
	references int
}

// trackedSnapshot is a snapshot and the number of queries currently reading it.
type trackedSnapshot struct {
	snapshot   Snapshot
	references int
}

// AddPod registers a newly written pod, making it queryable.
//
// Pods must arrive in block order and leave no gap. A gap would let a walk fall through the hole and answer
// from the floor, which is a wrong value rather than a failure, so a violation is a fault in the orderer
// upstream and is not something a caller can be asked to handle.
func (c *catalog) AddPod(pod *Pod) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if len(c.pods) > 0 {
		previous := c.pods[len(c.pods)-1].pod.Info
		if pod.Info.FirstBlock != previous.LastBlock+1 {
			panic(fmt.Sprintf("walrus: %s does not follow %s", pod.Info, previous))
		}
	}
	c.pods = append(c.pods, &trackedPod{pod: pod})
}

// AddSnapshot registers a newly retained snapshot.
func (c *catalog) AddSnapshot(snapshot Snapshot) {
	c.lock.Lock()
	defer c.lock.Unlock()

	tracked := &trackedSnapshot{snapshot: snapshot}
	position := len(c.snapshots)
	for position > 0 && c.snapshots[position-1].snapshot.BlockNumber() > snapshot.BlockNumber() {
		position--
	}
	c.snapshots = append(c.snapshots, nil)
	copy(c.snapshots[position+1:], c.snapshots[position:])
	c.snapshots[position] = tracked
}

// Query admits a read at blockNumber and holds back deletion of every file it may touch.
func (c *catalog) Query(blockNumber uint64) (Query, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if blockNumber < c.queryFloor {
		return nil, false
	}

	floor := c.floorLocked(blockNumber)
	floor.references++

	pods := make([]*Pod, 0, 8)
	floorBlock := floor.snapshot.BlockNumber()
	for index := len(c.pods) - 1; index >= 0; index-- {
		tracked := c.pods[index]
		if tracked.pod.Info.FirstBlock > blockNumber {
			continue
		}
		if tracked.pod.Info.LastBlock <= floorBlock {
			break
		}
		tracked.references++
		pods = append(pods, tracked.pod)
	}

	c.inFlight++
	return &catalogQuery{catalog: c, block: blockNumber, floor: floor.snapshot, pods: pods}, true
}

// SetQueryFloor raises the oldest block that may be queried.
func (c *catalog) SetQueryFloor(blockNumber uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if blockNumber > c.queryFloor {
		c.queryFloor = blockNumber
	}
}

// Collect advances the floor toward the query floor and deletes everything below it that nobody is reading.
func (c *catalog) Collect() (files int, bytes int64, err error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.floorBlock = c.nextFloorLocked()

	var problems []error
	keptPods := c.pods[:0]
	for _, tracked := range c.pods {
		if tracked.pod.Info.LastBlock > c.floorBlock || tracked.references > 0 {
			keptPods = append(keptPods, tracked)
			continue
		}
		bytes += tracked.pod.Size()
		files += 3
		if err := tracked.pod.Delete(); err != nil {
			problems = append(problems, err)
		}
	}
	c.pods = keptPods

	keptSnapshots := c.snapshots[:0]
	for _, tracked := range c.snapshots {
		if tracked.snapshot.BlockNumber() >= c.floorBlock || tracked.references > 0 {
			keptSnapshots = append(keptSnapshots, tracked)
			continue
		}
		bytes += tracked.snapshot.Size()
		if tracked.snapshot.Path() != "" {
			files++
		}
		if err := tracked.snapshot.Delete(); err != nil {
			problems = append(problems, err)
		}
	}
	c.snapshots = keptSnapshots

	recordCollection(c.name, files, bytes)
	c.recordFloorsLocked()
	if len(problems) > 0 {
		return files, bytes, fmt.Errorf("failed to collect: %w", errors.Join(problems...))
	}
	return files, bytes, nil
}

// Bounds reports the range of blocks the catalog can answer for.
func (c *catalog) Bounds() (ok bool, first uint64, last uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if len(c.pods) == 0 {
		return false, 0, 0
	}
	return true, c.pods[0].pod.Info.FirstBlock, c.pods[len(c.pods)-1].pod.Info.LastBlock
}

// release drops the references one query took.
func (c *catalog) release(query *catalogQuery) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for _, tracked := range c.snapshots {
		if tracked.snapshot == query.floor {
			tracked.references--
			break
		}
	}
	for _, pod := range query.pods {
		for _, tracked := range c.pods {
			if tracked.pod == pod {
				tracked.references--
				break
			}
		}
	}
	c.inFlight--
}

// floorLocked returns the newest snapshot at or below blockNumber.
//
// There is always one: the ladder starts with the block zero pseudo-snapshot, and nothing at or above the
// floor is ever deleted.
func (c *catalog) floorLocked(blockNumber uint64) *trackedSnapshot {
	for index := len(c.snapshots) - 1; index >= 0; index-- {
		if c.snapshots[index].snapshot.BlockNumber() <= blockNumber {
			return c.snapshots[index]
		}
	}
	return c.snapshots[0]
}

// nextFloorLocked returns the block the floor should advance to.
//
// Only a snapshot backed by real data may become the floor. Advancing onto the pseudo-snapshot would let
// pods be deleted with nothing beneath them, and a walk falling into that gap answers ReadAbsent for a key
// that plainly had a value.
func (c *catalog) nextFloorLocked() uint64 {
	for index := len(c.snapshots) - 1; index >= 0; index-- {
		snapshot := c.snapshots[index].snapshot
		if _, pseudo := snapshot.(pseudoSnapshot); pseudo {
			continue
		}
		if snapshot.BlockNumber() <= c.queryFloor && snapshot.BlockNumber() > c.floorBlock {
			return snapshot.BlockNumber()
		}
	}
	return c.floorBlock
}

// recordFloorsLocked publishes where policy and the floor sit, and how many queries are running.
func (c *catalog) recordFloorsLocked() {
	recordFloors(c.name, c.queryFloor, c.floorBlock, c.inFlight)
}

// newCatalog creates a catalog holding the given pods and snapshots, with the block zero pseudo-snapshot as
// its floor.
func newCatalog(config *Config, pods []*Pod, snapshots []Snapshot) *catalog {
	created := &catalog{
		snapshots: []*trackedSnapshot{{snapshot: pseudoSnapshot{}}},
		name:      config.Name,
	}
	for _, pod := range pods {
		created.AddPod(pod)
	}
	for _, snapshot := range snapshots {
		created.AddSnapshot(snapshot)
	}
	return created
}

var _ Query = (*catalogQuery)(nil)

// catalogQuery is one admitted read and the references it holds.
type catalogQuery struct {
	catalog  *catalog
	block    uint64
	floor    Snapshot
	pods     []*Pod
	released bool
}

// Block returns the block being read.
func (q *catalogQuery) Block() uint64 {
	return q.block
}

// Floor returns the snapshot the walk terminates at.
func (q *catalogQuery) Floor() Snapshot {
	return q.floor
}

// Pods returns the pods a backwards walk visits, newest first.
func (q *catalogQuery) Pods() []*Pod {
	return q.pods
}

// Release ends the read and drops the references it holds.
func (q *catalogQuery) Release() {
	if q.released {
		return
	}
	q.released = true
	q.catalog.release(q)
}
