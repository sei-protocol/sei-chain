package p2p

import (
	"fmt"
	"net/url"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// newValidatorAddrs returns the address store of a router: it keeps the announcements that
// verifyValidatorAddr accepts against the Anchor state holds at the time they arrive.
// allowLocal admits loopback and link-local hosts.
func newValidatorAddrs(state *data.State, allowLocal bool) *giga.ValidatorAddrs {
	return giga.NewValidatorAddrs(func(ann *atypes.SignedValidatorAddrAnnouncement) bool {
		return verifyValidatorAddr(state.Anchor().Load(), ann, allowLocal).IsPresent()
	})
}

// verifyValidatorAddr returns the dial address that ann announces, or None if ann is refused:
// when it is not signed by a member of an Anchor epoch, when its signature or address is bad,
// or, unless allowLocal is set, when its host is loopback or link-local.
func verifyValidatorAddr(anchor data.Anchor, ann *atypes.SignedValidatorAddrAnnouncement, allowLocal bool) utils.Option[GigaNodeAddr] {
	if !inAnchorEpochs(anchor, ann.Key()) {
		return refuseValidatorAddr(ann, "signer is not in an Anchor epoch")
	}
	if err := ann.VerifySig(); err != nil {
		return refuseValidatorAddr(ann, fmt.Sprintf("bad signature: %v", err))
	}
	addr, err := parseValidatorAddr(ann.Msg())
	if err != nil {
		return refuseValidatorAddr(ann, err.Error())
	}
	if !allowLocal && utils.IsLoopbackOrLinkLocalURL(url.URL{Host: addr.HostPort.String()}) {
		return refuseValidatorAddr(ann, "loopback or link-local host")
	}
	return utils.Some(addr)
}

// refuseValidatorAddr logs the reason ann is dropped and returns None.
func refuseValidatorAddr(ann *atypes.SignedValidatorAddrAnnouncement, reason string) utils.Option[GigaNodeAddr] {
	logger.Debug("dropped validator address announcement", "validator", ann.Key(), "version", ann.Msg().Version, "reason", reason)
	return utils.None[GigaNodeAddr]()
}

// parseValidatorAddr decodes the node key and host:port of an announcement.
func parseValidatorAddr(msg atypes.ValidatorAddrAnnouncement) (GigaNodeAddr, error) {
	key, err := NodePublicKeyFromString(msg.NodeKey)
	if err != nil {
		return GigaNodeAddr{}, fmt.Errorf("bad node key: %w", err)
	}
	hp, err := tcp.ParseHostPort(msg.HostPort)
	if err != nil {
		return GigaNodeAddr{}, fmt.Errorf("bad host:port: %w", err)
	}
	addr := NodeAddress{NodeID: key.NodeID(), Hostname: hp.Hostname, Port: hp.Port}
	if err := addr.Validate(); err != nil {
		return GigaNodeAddr{}, fmt.Errorf("bad host:port: %w", err)
	}
	return GigaNodeAddr{Key: key, HostPort: hp}, nil
}
