package commands

import (
	"github.com/spf13/cobra"

	cfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

var (
	genesisHash []byte
)

// AddNodeFlags exposes some common configuration options from conf in the flag
// set for cmd. This is a convenience for commands embedding a Tendermint node.
func AddNodeFlags(cmd *cobra.Command, conf *cfg.Config) {
	// bind flags
	cmd.Flags().String("moniker", conf.Moniker, "node name")

	// mode flags
	cmd.Flags().String("mode", conf.Mode, "node mode (full | validator | seed)")

	// priv val flags
	cmd.Flags().String(
		"priv-validator-laddr",
		conf.PrivValidator.ListenAddr,
		"socket address to listen on for connections from external priv-validator process")

	// node flags

	cmd.Flags().BytesHexVar(
		&genesisHash,
		"genesis-hash",
		[]byte{},
		"optional SHA-256 hash of the genesis file")
	cmd.Flags().Int64("consensus.double-sign-check-height", conf.Consensus.DoubleSignCheckHeight,
		"how many blocks to look back to check existence of the node's "+
			"consensus votes before joining consensus")

	// abci flags
	cmd.Flags().String(
		"proxy-app",
		conf.ProxyApp,
		"proxy app address, or one of: 'kvstore',"+
			" 'persistent_kvstore', 'e2e' or 'noop' for local testing.")
	cmd.Flags().String("abci", conf.ABCI, "specify abci transport (socket | grpc)")

	// rpc flags
	cmd.Flags().String("rpc.laddr", conf.RPC.ListenAddress, "RPC listen address. Port required")
	cmd.Flags().Bool("rpc.unsafe", conf.RPC.Unsafe, "enabled unsafe rpc methods")
	cmd.Flags().String("rpc.pprof-laddr", conf.RPC.PprofListenAddress, "pprof listen address (https://golang.org/pkg/net/http/pprof)")

	// p2p flags
	cmd.Flags().String(
		"p2p.laddr",
		conf.P2P.ListenAddress,
		"node listen address. (0.0.0.0:0 means any interface, any port)")
	cmd.Flags().String("p2p.persistent-peers", conf.P2P.PersistentPeers, "comma-delimited ID@host:port persistent peers")
	cmd.Flags().Bool("p2p.upnp", conf.P2P.UPNP, "enable/disable UPNP port forwarding")
	cmd.Flags().Bool("p2p.pex", conf.P2P.PexReactor, "enable/disable Peer-Exchange")
	cmd.Flags().String("p2p.private-peer-ids", conf.P2P.PrivatePeerIDs, "comma-delimited private peer IDs")
	cmd.Flags().String("p2p.unconditional_peer_ids",
		conf.P2P.UnconditionalPeerIDs, "comma-delimited IDs of unconditional peers")

	// consensus flags
	cmd.Flags().Bool(
		"consensus.create-empty-blocks",
		conf.Consensus.CreateEmptyBlocks,
		"set this to false to only produce blocks when there are txs or when the AppHash changes")
	cmd.Flags().String(
		"consensus.create-empty-blocks-interval",
		conf.Consensus.CreateEmptyBlocksInterval.String(),
		"the possible interval between empty blocks")
	cmd.Flags().Bool(
		"consensus.gossip-tx-key-only",
		conf.Consensus.GossipTransactionKeyOnly,
		"set this to false to gossip entire data rather than just the key")

	addDBFlags(cmd, conf)
}

func addDBFlags(cmd *cobra.Command, conf *cfg.Config) {
	cmd.Flags().String(
		"db-backend",
		conf.DBBackend,
		"database backend: goleveldb | cleveldb | boltdb | rocksdb | badgerdb")
	cmd.Flags().String(
		"db-dir",
		conf.DBPath,
		"database directory")
}
