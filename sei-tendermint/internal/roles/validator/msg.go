package validator

import (
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

type Msg interface { isMsg() }

type SessionID struct {
	utils.ReadOnly
	raw [10]byte
}

func (s *SessionID) Raw() [10]byte { return s.raw }

func (s *SessionID) isMsg() {}

type Sig struct { inner ed25519.Sig }

type Signed[M Msg] struct {
	utils.ReadOnly
	msg M
	sig Sig
}
