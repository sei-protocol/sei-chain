package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	seidbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	tmstatesync "github.com/sei-protocol/sei-chain/sei-tendermint/statesync"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

// forkedValidator pairs a locally-generated consensus key with the on-chain
// validator whose consensus identity it takes over.
type forkedValidator struct {
	pv          *privval.FilePV
	operator    sdk.ValAddress
	power       int64
	oldConsAddr sdk.ConsAddress
	newConsAddr sdk.ConsAddress
}

// ForkLocalnetCmd turns a copied flatkv-only data directory into the starting
// state of a brand-new local chain controlled by freshly generated validator
// keys. It is the tooling behind running production-scale state on a local
// cluster: convert a memiavl node with `seidb import-flatkv-from-memiavl
// --modules all --mark-as-migrated all`, then run this command once on the
// converted home, then clone the home to every validator.
func ForkLocalnetCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fork-localnet",
		Short: "Fork a flatkv-only data directory into a new locally-controlled chain",
		Long: `Fork a flatkv-only data directory into a new locally-controlled chain.

The command performs three steps on a single node home:
 1. generates N fresh consensus keys and remaps the top-N staked validators'
    consensus identities onto them inside the application state (staking
    validator records, consensus-address indexes, slashing signing infos, and
    the distribution previous-proposer record), committing one synthetic
    application block;
 2. computes the resulting application hash by loading the application store;
 3. forges the matching Tendermint state: a synthetic block and commit at the
    fork height signed by the new keys, a bootstrapped state record whose
    validator sets contain exactly the new keys, and a new genesis document.

The resulting home must be cloned byte-for-byte to every validator of the new
chain; each clone then receives one of the generated priv_validator_key.json
files. Do NOT rerun this command per node: the forged records must be
identical on all nodes.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			if err := serverCtx.Viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}
			home := serverCtx.Viper.GetString(flags.FlagHome)
			newChainID, _ := cmd.Flags().GetString("new-chain-id")
			validatorCount, _ := cmd.Flags().GetInt("validator-count")
			keysDir, _ := cmd.Flags().GetString("keys-dir")

			if newChainID == "" {
				return fmt.Errorf("--new-chain-id is required")
			}
			if validatorCount <= 0 {
				return fmt.Errorf("--validator-count must be positive, got %d", validatorCount)
			}
			if keysDir == "" {
				keysDir = filepath.Join(home, "fork-validators")
			}

			ctx := cmd.Context()

			validators, err := generateForkValidatorKeys(keysDir, validatorCount)
			if err != nil {
				return err
			}

			forkHeight, err := remapValidatorsInFlatKV(cmd, home, validators)
			if err != nil {
				return err
			}

			// app.New requires a chain ID in the app options; the forked home
			// starts life on the new chain, and the value does not affect the
			// computed commit hash.
			serverCtx.Viper.Set(flags.FlagChainID, newChainID)

			// A forked home is definitionally flatkv-only, but a converted
			// source usually still carries the pre-conversion app.toml
			// (sc-write-mode = "memiavl_only" under auto). Under auto the
			// composite store keeps a memiavl handle open in version
			// lockstep even when metadata derives FlatKVOnly, and with the
			// memiavl directory gone that handle is a fresh store at
			// version 0 — the app would report version 0 and the first
			// commit would fail the lockstep check. Pin the mode both for
			// the in-process app load below and, via app.toml, for every
			// node the home is cloned to.
			serverCtx.Viper.Set(app.FlagSCWriteMode, string(sctypes.FlatKVOnly))
			serverCtx.Viper.Set(app.FlagSCWriteModeEnableAuto, false)
			if err := pinFlatKVOnlyWriteMode(home); err != nil {
				return err
			}

			appHash, appVersion, err := loadForkedAppHash(cmd, home, forkHeight)
			if err != nil {
				return err
			}

			// serverCtx.Config carries the node's real config.toml (notably
			// db_dir, which sei sets to data/tendermint); a default config
			// would write the forged state/blockstore DBs to the wrong path.
			tmConfig := serverCtx.Config
			tmConfig.SetRoot(home)
			// The copied home still carries the source chain's consensus
			// databases; the forged records must land in empty stores or the
			// old chain's blocks and state would shadow them.
			if err := resetTendermintData(tmConfig); err != nil {
				return err
			}
			pvs := make([]tmtypes.PrivValidator, len(validators))
			powers := make([]int64, len(validators))
			for i, v := range validators {
				pvs[i] = v.pv
				powers[i] = v.power
			}
			if err := tmstatesync.BootstrapForkedChain(ctx, tmConfig, tmstatesync.ForkParams{
				ChainID:         newChainID,
				Height:          forkHeight,
				AppHash:         appHash,
				Validators:      pvs,
				Powers:          powers,
				ConsensusParams: *tmtypes.DefaultConsensusParams(),
				AppVersion:      appVersion,
				Time:            time.Now().UTC(),
			}); err != nil {
				return fmt.Errorf("bootstrap forked tendermint state: %w", err)
			}

			// The cloned home carries the source chain's client.toml; seid
			// start refuses to boot when its chain-id disagrees.
			if err := rewriteClientChainID(home, newChainID); err != nil {
				return err
			}

			cmd.Printf("Forked chain %q at height %d (app hash %X)\n", newChainID, forkHeight, appHash)
			cmd.Printf("Remapped validators:\n")
			for i, v := range validators {
				cmd.Printf("  node%d: operator=%s power=%d consensus-key=%s\n",
					i, v.operator.String(), v.power, filepath.Join(keysDir, fmt.Sprintf("node%d", i), "priv_validator_key.json"))
			}
			cmd.Printf("\nNext steps:\n")
			cmd.Printf("  1. clone %s to every validator node\n", home)
			cmd.Printf("  2. install %s/node<i>/priv_validator_key.json and priv_validator_state.json on node i\n", keysDir)
			cmd.Printf("  3. configure persistent peers between the nodes and start them together\n")
			return nil
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "Node home containing the converted flatkv-only data directory")
	cmd.Flags().String("new-chain-id", "", "Chain ID for the forked chain (must differ from the source chain)")
	cmd.Flags().Int("validator-count", 4, "Number of local validators to control the forked chain")
	cmd.Flags().String("keys-dir", "", "Directory for generated validator keys (default <home>/fork-validators)")
	return cmd
}

// pinFlatKVOnlyWriteMode rewrites config/app.toml so the home is explicitly
// configured as flatkv_only with auto derivation disabled. See the caller for
// why a converted home must not run under auto once memiavl is removed.
func pinFlatKVOnlyWriteMode(home string) error {
	path := filepath.Join(home, "config", "app.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	updated := raw
	writeModeRe := regexp.MustCompile(`(?m)^(\s*)sc-write-mode\s*=.*$`)
	if !writeModeRe.Match(updated) {
		return fmt.Errorf("%s has no state-commit.sc-write-mode key to pin", path)
	}
	updated = writeModeRe.ReplaceAll(updated, []byte(`${1}sc-write-mode = "flatkv_only"`))
	autoRe := regexp.MustCompile(`(?m)^(\s*)sc-write-mode-enable-auto\s*=.*$`)
	if autoRe.Match(updated) {
		updated = autoRe.ReplaceAll(updated, []byte("${1}sc-write-mode-enable-auto = false"))
	} else {
		updated = writeModeRe.ReplaceAll(updated,
			[]byte("${1}sc-write-mode = \"flatkv_only\"\n${1}sc-write-mode-enable-auto = false"))
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// rewriteClientChainID points config/client.toml at the new chain ID. A
// missing client.toml is fine; a stale one would make seid start refuse to
// boot with a chain-id mismatch.
func rewriteClientChainID(home, chainID string) error {
	path := filepath.Join(home, "config", "client.toml")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	re := regexp.MustCompile(`(?m)^chain-id\s*=.*$`)
	updated := re.ReplaceAll(raw, fmt.Appendf(nil, "chain-id = %q", chainID))
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// resetTendermintData deletes the consensus databases and WAL copied from the
// source chain so the forged records are written into empty stores. Leaving
// them in place is not just clutter: the old chain's state.db still resolves
// validator-set lookups below the fork height, so blocks after the fork would
// read the source chain's validator set instead of the forged one.
func resetTendermintData(cfg *tmcfg.Config) error {
	// Resolve each database exactly the way DefaultDBProvider will when the
	// forged records are written (data/tendermint/ vs the legacy flat layout).
	stale := make([]string, 0, 7)
	for _, id := range []string{"blockstore", "state", "evidence", "tx_index", "peerstore"} {
		stale = append(stale, filepath.Join(tmcfg.ResolveDBDir(id, cfg.DBDir()), id+".db"))
	}
	stale = append(stale, filepath.Dir(cfg.Consensus.WalFile()))
	for _, path := range stale {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove stale consensus data %s: %w", path, err)
		}
	}
	return nil
}

// generateForkValidatorKeys creates count fresh ed25519 consensus keys under
// keysDir/node<i>/ and returns them wrapped in forkedValidator stubs (operator
// assignment happens during the flatkv surgery).
func generateForkValidatorKeys(keysDir string, count int) ([]*forkedValidator, error) {
	validators := make([]*forkedValidator, count)
	for i := 0; i < count; i++ {
		nodeDir := filepath.Join(keysDir, fmt.Sprintf("node%d", i))
		if err := os.MkdirAll(nodeDir, 0o700); err != nil {
			return nil, fmt.Errorf("create key dir %s: %w", nodeDir, err)
		}
		keyPath := filepath.Join(nodeDir, "priv_validator_key.json")
		statePath := filepath.Join(nodeDir, "priv_validator_state.json")
		if _, err := os.Stat(keyPath); err == nil {
			return nil, fmt.Errorf("validator key %s already exists; refusing to overwrite", keyPath)
		}
		pv, err := privval.GenFilePV(keyPath, statePath, tmtypes.ABCIPubKeyTypeEd25519)
		if err != nil {
			return nil, fmt.Errorf("generate validator key %d: %w", i, err)
		}
		if err := pv.Save(); err != nil {
			return nil, fmt.Errorf("save validator key %d: %w", i, err)
		}
		validators[i] = &forkedValidator{pv: pv}
	}
	return validators, nil
}

// remapValidatorsInFlatKV rewrites the consensus identities of the top-N
// staked validators to the generated keys, directly against the flatkv commit
// store, and commits one synthetic application block. Returns the resulting
// (post-commit) application height.
//
// Only consensus identity moves: operator addresses, delegations, tokens, and
// commission all stay with the original validators, so no staking power
// changes and the application never emits validator-set updates that would
// diverge from the forged Tendermint validator set.
func remapValidatorsInFlatKV(cmd *cobra.Command, home string, validators []*forkedValidator) (int64, error) {
	ctx := cmd.Context()
	cdc := app.MakeEncodingConfig().Marshaler

	// A leftover memiavl directory is fatal, not just wasteful: when the app
	// later loads under sc-write-mode auto/memiavl_only it constructs BOTH
	// backends, and the composite store's crash reconciliation sees memiavl
	// at the pre-fork height and the fork surgery as flatkv being "one
	// commit ahead" — and rolls the surgery back. Refuse up front.
	memiavlDir := seidbutils.GetCosmosSCStorePath(home)
	if seidbutils.DirExists(memiavlDir) {
		return 0, fmt.Errorf("memiavl directory %s still exists; a fully converted flatkv_only home "+
			"must not carry memiavl or startup version reconciliation will roll the fork commit back — "+
			"delete it and rerun", memiavlDir)
	}

	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = seidbutils.GetFlatKVPath(home)
	store, err := flatkv.NewCommitStore(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("create flatkv store: %w", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.LoadVersion(0, false); err != nil {
		return 0, fmt.Errorf("open flatkv store: %w", err)
	}

	// The store must hold ALL module data. That is guaranteed either by
	// migration-complete metadata (converted homes: import-flatkv-from-memiavl
	// --mark-as-migrated all) or by the node having been explicitly configured
	// to write flatkv_only since genesis (no migration metadata exists then).
	mode, err := migration.DeriveWriteMode(store)
	if err != nil {
		return 0, fmt.Errorf("derive write mode: %w", err)
	}
	if mode != sctypes.FlatKVOnly {
		serverCtx := server.GetServerContextFromCmd(cmd)
		enableAuto := true
		if v := serverCtx.Viper.Get(app.FlagSCWriteModeEnableAuto); v != nil {
			enableAuto = cast.ToBool(v)
		}
		configuredMode := sctypes.WriteMode(cast.ToString(serverCtx.Viper.Get(app.FlagSCWriteMode)))
		if enableAuto || configuredMode != sctypes.FlatKVOnly {
			return 0, fmt.Errorf("home %s derives write mode %q and app.toml does not pin sc-write-mode = "+
				"\"flatkv_only\"; fork-localnet requires a store holding all modules "+
				"(run seidb import-flatkv-from-memiavl --modules all --mark-as-migrated all first)", home, mode)
		}
	}
	height := store.Version()
	if height <= 1 {
		return 0, fmt.Errorf("flatkv store at version %d has no state to fork", height)
	}

	type lastPower struct {
		operator sdk.ValAddress
		power    int64
	}
	var powersList []lastPower
	iter, err := store.Iterator(keys.StakingStoreKey,
		stakingtypes.LastValidatorPowerKey, sdk.PrefixEndBytes(stakingtypes.LastValidatorPowerKey), true)
	if err != nil {
		return 0, fmt.Errorf("iterate last validator powers: %w", err)
	}
	for ; iter.Valid(); iter.Next() {
		intV := gogotypes.Int64Value{}
		cdc.MustUnmarshal(iter.Value(), &intV)
		operator := make([]byte, len(stakingtypes.AddressFromLastValidatorPowerKey(iter.Key())))
		copy(operator, stakingtypes.AddressFromLastValidatorPowerKey(iter.Key()))
		powersList = append(powersList, lastPower{operator: operator, power: intV.GetValue()})
	}
	if err := iter.Error(); err != nil {
		_ = iter.Close()
		return 0, fmt.Errorf("iterate last validator powers: %w", err)
	}
	_ = iter.Close()
	if len(powersList) < len(validators) {
		return 0, fmt.Errorf("app state has %d bonded validators but %d fork validators requested",
			len(powersList), len(validators))
	}
	sort.SliceStable(powersList, func(i, j int) bool {
		if powersList[i].power != powersList[j].power {
			return powersList[i].power > powersList[j].power
		}
		return bytes.Compare(powersList[i].operator, powersList[j].operator) < 0
	})

	var stakingPairs, slashingPairs, distrPairs []*seidbproto.KVPair
	for i, fv := range validators {
		fv.operator = powersList[i].operator
		fv.power = powersList[i].power

		valKey := stakingtypes.GetValidatorKey(fv.operator)
		valBz, found := store.Get(keys.StakingStoreKey, valKey)
		if !found {
			return 0, fmt.Errorf("validator record for operator %X not found", fv.operator)
		}
		var validator stakingtypes.Validator
		if err := cdc.Unmarshal(valBz, &validator); err != nil {
			return 0, fmt.Errorf("unmarshal validator %X: %w", fv.operator, err)
		}
		oldConsAddr, err := validator.GetConsAddr()
		if err != nil {
			return 0, fmt.Errorf("consensus address of validator %X: %w", fv.operator, err)
		}
		fv.oldConsAddr = oldConsAddr

		tmPubKey, err := fv.pv.GetPubKey(ctx)
		if err != nil {
			return 0, fmt.Errorf("fork validator %d pubkey: %w", i, err)
		}
		sdkPubKey, err := cryptocodec.FromTmPubKeyInterface(tmPubKey)
		if err != nil {
			return 0, fmt.Errorf("convert fork validator %d pubkey: %w", i, err)
		}
		pkAny, err := codectypes.NewAnyWithValue(sdkPubKey)
		if err != nil {
			return 0, fmt.Errorf("pack fork validator %d pubkey: %w", i, err)
		}
		validator.ConsensusPubkey = pkAny
		fv.newConsAddr = sdk.ConsAddress(sdkPubKey.Address())

		newValBz, err := cdc.Marshal(&validator)
		if err != nil {
			return 0, fmt.Errorf("marshal validator %X: %w", fv.operator, err)
		}
		stakingPairs = append(stakingPairs,
			&seidbproto.KVPair{Key: valKey, Value: newValBz},
			&seidbproto.KVPair{Key: stakingtypes.GetValidatorByConsAddrKey(fv.oldConsAddr), Delete: true},
			&seidbproto.KVPair{Key: stakingtypes.GetValidatorByConsAddrKey(fv.newConsAddr), Value: fv.operator.Bytes()},
		)

		signingInfo := slashingtypes.ValidatorSigningInfo{
			Address:             fv.newConsAddr.String(),
			StartHeight:         height,
			IndexOffset:         0,
			JailedUntil:         time.Unix(0, 0).UTC(),
			Tombstoned:          false,
			MissedBlocksCounter: 0,
		}
		signingInfoBz, err := cdc.Marshal(&signingInfo)
		if err != nil {
			return 0, fmt.Errorf("marshal signing info for %s: %w", fv.newConsAddr, err)
		}
		// Slashing's BeginBlocker resolves each vote's consensus address
		// through its own address→pubkey relation (AddPubkey at validator
		// creation), so the relation must move with the identity.
		pubKeyRelationBz, err := cdc.MarshalInterface(sdkPubKey)
		if err != nil {
			return 0, fmt.Errorf("marshal pubkey relation for %s: %w", fv.newConsAddr, err)
		}
		slashingPairs = append(slashingPairs,
			&seidbproto.KVPair{Key: slashingtypes.ValidatorSigningInfoKey(fv.oldConsAddr), Delete: true},
			&seidbproto.KVPair{Key: slashingtypes.ValidatorSigningInfoKey(fv.newConsAddr), Value: signingInfoBz},
			&seidbproto.KVPair{Key: slashingtypes.AddrPubkeyRelationKey(fv.oldConsAddr), Delete: true},
			&seidbproto.KVPair{Key: slashingtypes.AddrPubkeyRelationKey(fv.newConsAddr), Value: pubKeyRelationBz},
		)
	}

	// The first forked block's distribution BeginBlocker resolves the
	// previous-proposer record against ValidatorByConsAddr; point it at a
	// remapped validator so the lookup cannot hit a deleted index.
	proposerBz, err := cdc.Marshal(&gogotypes.BytesValue{Value: validators[0].newConsAddr.Bytes()})
	if err != nil {
		return 0, fmt.Errorf("marshal previous proposer: %w", err)
	}
	distrPairs = append(distrPairs, &seidbproto.KVPair{Key: distrtypes.ProposerKey, Value: proposerBz})

	if err := store.ApplyChangeSets([]*seidbproto.NamedChangeSet{
		{Name: keys.StakingStoreKey, Changeset: seidbproto.ChangeSet{Pairs: stakingPairs}},
		{Name: keys.SlashingStoreKey, Changeset: seidbproto.ChangeSet{Pairs: slashingPairs}},
		{Name: keys.DistributionStoreKey, Changeset: seidbproto.ChangeSet{Pairs: distrPairs}},
	}); err != nil {
		return 0, fmt.Errorf("apply fork surgery changesets: %w", err)
	}
	newVersion, err := store.Commit()
	if err != nil {
		return 0, fmt.Errorf("commit fork surgery: %w", err)
	}
	if err := store.Close(); err != nil {
		return 0, fmt.Errorf("close flatkv store: %w", err)
	}
	cmd.Printf("Remapped %d validator consensus identities at flatkv version %d\n", len(validators), newVersion)
	return newVersion, nil
}

// loadForkedAppHash loads the application over the post-surgery store and
// returns its committed app hash and app version at expectedHeight. Going
// through app.New (rather than recomputing the commit-info hash by hand)
// guarantees the hash includes the same amended store infos the running node
// will report during the ABCI handshake.
func loadForkedAppHash(cmd *cobra.Command, home string, expectedHeight int64) ([]byte, uint64, error) {
	serverCtx := server.GetServerContextFromCmd(cmd)
	a := app.New(
		nil,
		nil,
		true,
		map[int64]bool{},
		home,
		0,
		true,
		nil,
		app.MakeEncodingConfig(),
		wasm.EnableAllProposals,
		serverCtx.Viper,
		nil,
		app.EmptyAppOptions,
	)
	defer func() { _ = a.Close() }()

	commitID := a.CommitMultiStore().LastCommitID()
	if commitID.Version != expectedHeight {
		return nil, 0, fmt.Errorf("application loaded at version %d, expected fork height %d",
			commitID.Version, expectedHeight)
	}
	if len(commitID.Hash) == 0 {
		return nil, 0, fmt.Errorf("application returned an empty commit hash at height %d", expectedHeight)
	}
	appHash := make([]byte, len(commitID.Hash))
	copy(appHash, commitID.Hash)
	return appHash, a.AppVersion(), nil
}
