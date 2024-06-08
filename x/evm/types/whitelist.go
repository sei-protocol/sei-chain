package types

import "github.com/ethereum/go-ethereum/common"

func (w *Whitelist) IsHashInWhiteList(h common.Hash) bool {
	for _, s := range w.Hashes {
		if s == h.Hex() {
			return true
		}
	}
	return false
}
