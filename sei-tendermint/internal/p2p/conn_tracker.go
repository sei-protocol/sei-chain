package p2p

import (
	"fmt"
	"net/netip"
	"sync"
	"time"
)

type connTracker struct {
	cache       map[netip.Addr]uint
	lastConnect map[netip.Addr]time.Time
	mutex       sync.RWMutex
	max         uint
	window      time.Duration
	nextSweep   time.Time
}

func newConnTracker(max uint, window time.Duration) *connTracker {
	return &connTracker{
		cache:       map[netip.Addr]uint{},
		lastConnect: map[netip.Addr]time.Time{},
		max:         max,
		window:      window,
	}
}

func (rat *connTracker) Len() int {
	rat.mutex.RLock()
	defer rat.mutex.RUnlock()
	return len(rat.cache)
}

// sweepLocked drops lastConnect entries whose window has elapsed.
func (rat *connTracker) sweepLocked(now time.Time) {
	// At most once per window. An entry is only consulted while it is inside the
	// window, so anything older is dead weight. RemoveConn drops an entry itself
	// when the connection outlived the window, which leaves exactly the addresses
	// whose connections died inside it: without this sweep those stay for the life
	// of the process, and on a public listener that set is unbounded.
	if now.Before(rat.nextSweep) {
		return
	}
	rat.nextSweep = now.Add(rat.window)
	for address, last := range rat.lastConnect {
		if now.Sub(last) > rat.window {
			delete(rat.lastConnect, address)
		}
	}
}

func (rat *connTracker) AddConn(addrPort netip.AddrPort) error {
	address := addrPort.Addr()
	rat.mutex.Lock()
	defer rat.mutex.Unlock()
	rat.sweepLocked(time.Now())

	if num := rat.cache[address]; num >= rat.max {
		return fmt.Errorf("%q has %d connections [max=%d]", address, num, rat.max)
	} else if num == 0 {
		// if there is already at least one connection, check to
		// see if it was established before within the window,
		// and error if so.
		if last := rat.lastConnect[address]; time.Since(last) < rat.window {
			return fmt.Errorf("%q tried to connect within window of last %s", address, rat.window)
		}
	}

	rat.cache[address]++
	rat.lastConnect[address] = time.Now()

	return nil
}

func (rat *connTracker) RemoveConn(addrPort netip.AddrPort) {
	address := addrPort.Addr()
	rat.mutex.Lock()
	defer rat.mutex.Unlock()
	rat.sweepLocked(time.Now())

	if num := rat.cache[address]; num > 0 {
		rat.cache[address]--
	}
	if num := rat.cache[address]; num <= 0 {
		delete(rat.cache, address)
	}

	if last, ok := rat.lastConnect[address]; ok && time.Since(last) > rat.window {
		delete(rat.lastConnect, address)
	}
}
