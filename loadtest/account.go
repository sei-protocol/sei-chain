package main

import "sync"


type AccountManager struct {
	AccountNum uint64
	SeqNum uint64
	SeqNumLock *sync.Mutex
}


func (account *AccountManager) GetNextSeqNumber() uint64 {
	account.SeqNumLock.Lock()
	account.SeqNum++
	println(account.SeqNum)
	return account.SeqNum
}
