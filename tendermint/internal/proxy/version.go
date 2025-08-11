package proxy

import (
	abci "github.com/sei-protocol/sei-chain/tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/tendermint/version"
)

// RequestInfo contains all the information for sending
// the abci.RequestInfo message during handshake with the app.
// It contains only compile-time version information.
var RequestInfo = abci.RequestInfo{
	Version:      version.TMVersion,
	BlockVersion: version.BlockProtocol,
	P2PVersion:   version.P2PProtocol,
	AbciVersion:  version.ABCIVersion,
}
