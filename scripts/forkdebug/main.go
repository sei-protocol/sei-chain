// forkdebug dumps the staking unbonding-validator queue (and each queued
// validator's live status) straight out of a flatkv commit store, plus the
// store's version and root hash. Debugging aid for forked-chain wedges: run
// it against a stopped node's home to see exactly which queue entry
// UnbondAllMatureValidators will panic on.
//
// Usage: forkdebug --home <node-home>
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

func main() {
	home := flag.String("home", "", "node home")
	writesAt := flag.Int64("writes-at", 0, "dump every physical key whose stored value was written at this block height")
	flag.Parse()
	if *home == "" {
		fmt.Fprintln(os.Stderr, "--home required")
		os.Exit(1)
	}
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = seidbutils.GetFlatKVPath(*home)
	base, err := flatkv.NewCommitStore(context.Background(), cfg)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = base.Close() }()
	loaded, err := base.LoadVersion(0, true)
	if err != nil {
		fatal(err)
	}
	store, ok := loaded.(*flatkv.CommitStore)
	if !ok {
		fatal(fmt.Errorf("unexpected store type %T", loaded))
	}
	defer func() { _ = store.Close() }()
	fmt.Printf("flatkv version=%d rootHash=%X\n", store.Version(), store.CommittedRootHash())

	if *writesAt > 0 {
		dumpWritesAtHeight(store, *writesAt)
		return
	}

	iter, err := store.Iterator(keys.StakingStoreKey,
		stakingtypes.ValidatorQueueKey, sdk.PrefixEndBytes(stakingtypes.ValidatorQueueKey), true)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = iter.Close() }()
	entries := 0
	for ; iter.Valid(); iter.Next() {
		keyTime, keyHeight, err := stakingtypes.ParseValidatorQueueKey(iter.Key())
		if err != nil {
			fatal(err)
		}
		var addrs stakingtypes.ValAddresses
		cdc.MustUnmarshal(iter.Value(), &addrs)
		for _, bech := range addrs.Addresses {
			valAddr, err := sdk.ValAddressFromBech32(bech)
			if err != nil {
				fatal(err)
			}
			status := "MISSING"
			unbTime := ""
			unbHeight := int64(-1)
			jailed := false
			if valBz, found := store.Get(keys.StakingStoreKey, stakingtypes.GetValidatorKey(valAddr)); found {
				var v stakingtypes.Validator
				if err := cdc.Unmarshal(valBz, &v); err != nil {
					fatal(err)
				}
				status = v.Status.String()
				unbTime = v.UnbondingTime.UTC().Format("2006-01-02T15:04:05Z")
				unbHeight = v.UnbondingHeight
				jailed = v.Jailed
			}
			_, getFound := store.Get(keys.StakingStoreKey, iter.Key())
			fmt.Printf("entry keyTime=%s keyHeight=%d addr=%s status=%s jailed=%v valUnbTime=%s valUnbHeight=%d getFound=%v\n",
				keyTime.UTC().Format("2006-01-02T15:04:05Z"), keyHeight, bech, status, jailed, unbTime, unbHeight, getFound)
		}
		entries++
	}
	if err := iter.Error(); err != nil {
		fatal(err)
	}
	fmt.Printf("total queue keys=%d\n", entries)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fatal:", err)
	os.Exit(1)
}

// dumpWritesAtHeight scans every committed physical key across the four data
// DBs and prints the ones whose stored value records blockHeight == target
// (every flatkv value type stamps the height it was last written at). Output
// lines are "W <physKeyHex> <sha256(value)[:8]>" so two nodes' dumps can be
// diffed directly to isolate a divergent block's write-set.
func dumpWritesAtHeight(store *flatkv.CommitStore, target int64) {
	iter, err := store.RawGlobalIterator()
	if err != nil {
		fatal(err)
	}
	defer func() { _ = iter.Close() }()

	evmPrefix := []byte("evm/")
	scanned, matched := 0, 0
	for ; iter.Valid(); iter.Next() {
		key, val := iter.Key(), iter.Value()
		scanned++
		if scanned%50_000_000 == 0 {
			fmt.Fprintf(os.Stderr, "scanned %d keys...\n", scanned)
		}
		var height int64
		if bytes.HasPrefix(key, evmPrefix) {
			kind, _, err := ktype.StripEVMPhysicalKey(key)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip unparseable evm key %x: %v\n", key, err)
				continue
			}
			switch kind {
			case ktype.EVMKeyAccount:
				d, err := vtype.DeserializeAccountData(val)
				if err != nil {
					fatal(fmt.Errorf("account %x: %w", key, err))
				}
				height = d.GetBlockHeight()
			case keys.EVMKeyStorage:
				d, err := vtype.DeserializeStorageData(val)
				if err != nil {
					fatal(fmt.Errorf("storage %x: %w", key, err))
				}
				height = d.GetBlockHeight()
			case keys.EVMKeyCode:
				d, err := vtype.DeserializeCodeData(val)
				if err != nil {
					fatal(fmt.Errorf("code %x: %w", key, err))
				}
				height = d.GetBlockHeight()
			default: // misc lane (incl. evm-misc)
				d, err := vtype.DeserializeMiscData(val)
				if err != nil {
					fatal(fmt.Errorf("evm misc %x: %w", key, err))
				}
				height = d.GetBlockHeight()
			}
		} else {
			d, err := vtype.DeserializeMiscData(val)
			if err != nil {
				fatal(fmt.Errorf("misc %x: %w", key, err))
			}
			height = d.GetBlockHeight()
		}
		if height != target {
			continue
		}
		sum := sha256.Sum256(val)
		fmt.Printf("W %x %x\n", key, sum[:8])
		matched++
	}
	if err := iter.Error(); err != nil {
		fatal(err)
	}
	fmt.Printf("scanned=%d matched=%d at height %d\n", scanned, matched, target)
}
