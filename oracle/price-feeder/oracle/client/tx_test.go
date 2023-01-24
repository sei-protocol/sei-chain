package client

import (
	"github.com/stretchr/testify/assert"
	"math/rand"
	"sync"
	"testing"
	"time"
)

var (
	accountNumber  = uint64(1)
	sequenceNumber = uint64(1)
)

func TestAccountSequenceNumberMonotonicallyIncrease(t *testing.T) {
	var sequenceList []uint64
	var lock = sync.Mutex{}
	rand.Seed(time.Now().UnixNano())
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			for i := 0; i < 20; i++ {
				_, accountSequence := mockPrepareFactory()
				lock.Lock()
				sequenceList = append(sequenceList, accountSequence)
				lock.Unlock()
				time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	// Make sure sequence id is monotonically increasing
	for i := 0; i < len(sequenceList)-1; i++ {
		assert.Equal(t, sequenceList[i]+1, sequenceList[i+1])
	}

}

func mockGetAccountNumberSequence() (accountNum uint64, accountSeq uint64) {
	now := time.Now().UnixMicro()
	sequenceNumber += uint64(now % 3)
	return accountNumber, sequenceNumber
}

func mockPrepareFactory() (accountNum uint64, accountSeq uint64) {
	accountNumber, accountSequence := mockGetAccountNumberSequence()
	if !AtomicSequenceNumber.CompareAndSwap(0, accountSequence) {
		accountSequence = AtomicSequenceNumber.Add(1)
	}
	return accountNumber, accountSequence
}
