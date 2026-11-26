package p2p

import (
	"errors"
	"fmt"
	"net/url"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

var (
	errAddrBadSignature      = errors.New("bad signature")
	errAddrSignerNotInAnchor = errors.New("signer is not in an Anchor epoch")
	errAddrBadNodeKey        = errors.New("bad node key")
	errAddrBadHostPort       = errors.New("bad host:port")
	errAddrLocalHost         = errors.New("loopback or link-local host")
)

// newValidatorAddrs returns the address store of a router: it keeps the announcements that
// verifyValidatorAddr accepts against the Anchor state holds at the time they arrive.
// allowLocal admits loopback and link-local hosts.
func newValidatorAddrs(state *data.State, allowLocal bool) *giga.ValidatorAddrs[GigaNodeAddr] {
	return giga.NewValidatorAddrs(func(ann *atypes.Signed[atypes.ValidatorAddrAnnouncement]) (GigaNodeAddr, error) {
		return verifyValidatorAddr(state.Anchor().Load(), ann, allowLocal)
	})
}

// verifyValidatorAddr returns the dial address that ann announces, after checking that ann is
// signed by a member of an Anchor epoch. Unless allowLocal is set, it refuses loopback and
// link-local hosts.
func verifyValidatorAddr(anchor data.Anchor, ann *atypes.Signed[atypes.ValidatorAddrAnnouncement], allowLocal bool) (GigaNodeAddr, error) {
	if !inAnchorEpochs(anchor, ann.Key()) {
		return GigaNodeAddr{}, fmt.Errorf("%w: %v", errAddrSignerNotInAnchor, ann.Key())
	}
	if err := ann.VerifySig(); err != nil {
		return GigaNodeAddr{}, fmt.Errorf("%w: %w", errAddrBadSignature, err)
	}
	addr, err := parseValidatorAddr(ann.Msg())
	if err != nil {
		return GigaNodeAddr{}, err
	}
	if !allowLocal && utils.IsLoopbackOrLinkLocalURL(url.URL{Host: addr.HostPort.String()}) {
		return GigaNodeAddr{}, fmt.Errorf("%w: %v", errAddrLocalHost, addr.HostPort)
	}
	return addr, nil
}

// parseValidatorAddr decodes the node key and host:port of an announcement.
func parseValidatorAddr(msg atypes.ValidatorAddrAnnouncement) (GigaNodeAddr, error) {
	key, err := NodePublicKeyFromString(msg.NodeKey)
	if err != nil {
		return GigaNodeAddr{}, fmt.Errorf("%w: %w", errAddrBadNodeKey, err)
	}
	hp, err := tcp.ParseHostPort(msg.HostPort)
	if err != nil {
		return GigaNodeAddr{}, fmt.Errorf("%w: %w", errAddrBadHostPort, err)
	}
	addr := NodeAddress{NodeID: key.NodeID(), Hostname: hp.Hostname, Port: hp.Port}
	if err := addr.Validate(); err != nil {
		return GigaNodeAddr{}, fmt.Errorf("%w: %w", errAddrBadHostPort, err)
	}
	return GigaNodeAddr{Key: key, HostPort: hp}, nil
}
