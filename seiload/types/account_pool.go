package types

import (
	"math/rand"
	"sync"
)

// AccountPool returns a next account for load generation.
type AccountPool interface {
	NextAccount() *Account
}

// AccountConfig stores the configuration for account generation.
type AccountConfig struct {
	Accounts       []*Account
	NewAccountRate float64
}

type accountPool struct {
	Accounts []*Account
	cfg      *AccountConfig

	mx  sync.Mutex
	idx int
}

func (a *accountPool) nextIndex() int {
	a.mx.Lock()
	defer a.mx.Unlock()
	a.idx++
	a.idx %= len(a.Accounts)
	return a.idx
}

// NextAccount returns the next account.
func (a *accountPool) NextAccount() *Account {
	if a.cfg.NewAccountRate > 0 {
		randomNumber := rand.Float64()
		if randomNumber <= a.cfg.NewAccountRate {
			return GenerateAccounts(1)[0]
		}
	}
	return a.Accounts[a.nextIndex()]
}

// NewAccountPool creates a new account generator from a config.
func NewAccountPool(cfg *AccountConfig) AccountPool {
	return &accountPool{
		Accounts: cfg.Accounts,
		cfg:      cfg,
	}
}
