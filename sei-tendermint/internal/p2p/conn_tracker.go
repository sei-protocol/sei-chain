package p2p

import (
	"fmt"
	"net/netip"
	"sync"
	"time"
)

type connectionTracker interface {
	AddConn(netip.AddrPort) error
	RemoveConn(netip.AddrPort)
	Len() int
}

type connTrackerImpl struct {
	cache       map[netip.Addr]uint
	lastConnect map[netip.Addr]time.Time
	mutex       sync.RWMutex
	max         uint
	window      time.Duration
}

func newConnTracker(max uint, window time.Duration) connectionTracker {
	return &connTrackerImpl{
		cache:       map[netip.Addr]uint{},
		lastConnect: map[netip.Addr]time.Time{},
		max:         max,
		window:      window,
	}
}

func (rat *connTrackerImpl) Len() int {
	rat.mutex.RLock()
	defer rat.mutex.RUnlock()
	return len(rat.cache)
}

func (rat *connTrackerImpl) AddConn(addrPort netip.AddrPort) error {
	address := addrPort.Addr()
	rat.mutex.Lock()
	defer rat.mutex.Unlock()

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

func (rat *connTrackerImpl) RemoveConn(addrPort netip.AddrPort) {
	address := addrPort.Addr()
	rat.mutex.Lock()
	defer rat.mutex.Unlock()

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
