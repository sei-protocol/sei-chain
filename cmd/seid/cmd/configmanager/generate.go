package configmanager

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

const (
	// flagGenerateMode is spelled here rather than shared with the provisioning command, whose package
	// imports this one.
	flagGenerateMode       = "mode"
	flagGenerateWrite      = "write"
	flagGenerateFromLegacy = "from-legacy"
)

// GenerateCmd returns `seid config generate`, which writes a sei.toml for one kind of node. With no source
// the file states only the schema version and node mode, so every declared key resolves from this
// binary's defaults. With --from-legacy it also states each key this node's app.toml and config.toml
// answer differently from those defaults, so a node adopting the file runs what it ran before. The
// environment is not read: a variable keeps answering its key at the same precedence either way.
func GenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Write a sei.toml for a kind of node",
		Long: "Writes a sei.toml for the kind of node --mode names. By default it states only the schema " +
			"version and node mode, so every setting takes this binary's default for that kind.\n\n" +
			"--from-legacy reads this node's app.toml and config.toml the way a boot reads them and also " +
			"states every setting they answer differently, so a node adopting the file runs what it runs " +
			"today.\n\n" +
			"Prints the file by default. Pass --write to place it in the node's config directory; an " +
			"existing sei.toml is never replaced.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		// Replaces the root hook, which would create missing legacy files and copy their values into flags
		// before this reads them.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := theHomeThisCommandRuns(cmd)
			if err != nil {
				return err
			}
			mode, err := theModeTheWrittenValuesResolveFor(cmd)
			if err != nil {
				return err
			}
			fromLegacy, err := cmd.Flags().GetBool(flagGenerateFromLegacy)
			if err != nil {
				return err
			}
			stated := map[string]any{}
			if fromLegacy {
				stated, err = whatTheLegacyFilesStateBeyondTheDeclaration(cmd, home, mode)
			} else {
				err = theKindTheNodesOwnFileRecords(home, mode)
			}
			if err != nil {
				return err
			}
			file, err := theFileStating(mode, stated)
			if err != nil {
				return err
			}
			return thisFilePlacedOrPrinted(cmd, home, file, len(stated))
		},
	}
	cmd.Flags().String(flagGenerateMode, "", "the kind of node the written values resolve for: "+
		modesInOrder())
	cmd.Flags().Bool(flagGenerateFromLegacy, false,
		"also state every setting this node's app.toml and config.toml answer differently from the defaults")
	cmd.Flags().Bool(flagGenerateWrite, false,
		"place the file at config/"+seiTomlName+" instead of printing it")
	return cmd
}

// whatTheLegacyFilesStateBeyondTheDeclaration returns the declared keys this node's legacy files answer
// differently from mode's defaults, refusing a home a boot would not read them from.
func whatTheLegacyFilesStateBeyondTheDeclaration(cmd *cobra.Command, home string,
	mode registry.Mode) (map[string]any, error) {
	if err := theHomeHoldsANodeToDescribe(home); err != nil {
		return nil, err
	}
	if err := noSeiTomlIsInUse(home); err != nil {
		return nil, err
	}
	own, err := theNodesOwnConfiguration(home)
	if err != nil {
		return nil, fmt.Errorf("read this node's own configuration: %w", err)
	}
	if err := theKindThisNodeAlreadyRuns(mode, own); err != nil {
		return nil, err
	}
	running, err := whatThisNodeAlreadyRuns(cmd, home, own)
	if err != nil {
		return nil, err
	}
	return whatTheDeclarationDoesNotAlreadySay(mode, running)
}

// theModeTheWrittenValuesResolveFor reads the required --mode. It is not guessed, because the kinds differ
// on whether a node prunes and whether it serves queries.
func theModeTheWrittenValuesResolveFor(cmd *cobra.Command) (registry.Mode, error) {
	given, err := cmd.Flags().GetString(flagGenerateMode)
	if err != nil {
		return "", err
	}
	if given == "" {
		return "", fmt.Errorf("--%s is required: every value written here resolves for one kind of node, "+
			"and the kinds differ on whether a node prunes and whether it serves queries. One of %s",
			flagGenerateMode, modesInOrder())
	}
	for _, known := range registry.Modes() {
		if registry.Mode(given) == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("%q is not a kind of node this binary declares defaults for. One of %s",
		given, modesInOrder())
}

// modesInOrder names every kind of node, for a message that has to list them.
func modesInOrder() string {
	names := make([]string, 0, len(registry.Modes()))
	for _, mode := range registry.Modes() {
		names = append(names, string(mode))
	}
	return strings.Join(names, ", ")
}

// KeysABootLeavesToTheirReader is every declared key a lookup delivers that a boot-generated app.toml does
// not state. Each is declared from the default its reader falls back to, so a file leaving it out moves
// nothing.
var KeysABootLeavesToTheirReader = []string{
	"eth_replay.contract_state_checks",
	"evm.enable_test_api",
	"evm.max_concurrent_simulation_calls",
	"evm.max_tx_pool_txs",
	"evm.rpc_stats_interval",
	"genesis.import-file",
	"state-commit.flatkv.enable-read-write-metrics",
	"state-commit.sc-snapshot-writer-limit",
	"state-commit.sc-write-mode-enable-auto",
	"wasm.memory_cache_size",
	"wasm.simulation_gas_limit",
}

// whatThisNodeAlreadyRuns returns the value this node answers for each declared key: decoded keys off the
// struct config.toml decodes into, the rest off app.toml over the start command's flag defaults. A lookup
// key nothing answers is refused unless it is in KeysABootLeavesToTheirReader, since its reader's own
// default, which the node runs today, need not be the declared value a file leaving it out would give it.
func whatThisNodeAlreadyRuns(cmd *cobra.Command, home string, own *tmcfg.Config) (map[string]any, error) {
	source, err := theSourceThisNodeWouldBuild(cmd, home)
	if err != nil {
		return nil, err
	}

	_, ownedByADecode := registry.ResolvedAndOwnedByDecodedSections(registry.Resolved{})
	decoded, unread, err := whatEachKeyHolds(own, ownedByADecode)
	if err != nil {
		return nil, err
	}
	if len(unread) > 0 {
		return nil, fmt.Errorf("%d of %d keys a decode delivers are not present in the node's "+
			"configuration, so a file written from it would leave them out and move them to their "+
			"declared value: %v", len(unread), len(ownedByADecode), unread)
	}

	running := make(map[string]any, len(registry.Keys()))
	for key, value := range decoded {
		running[key] = value
	}
	leftToTheirReader := make(map[string]bool, len(KeysABootLeavesToTheirReader))
	for _, key := range KeysABootLeavesToTheirReader {
		leftToTheirReader[key] = true
	}
	var unanswered []string
	for _, key := range registry.Keys() {
		if _, byADecode := decoded[key]; byADecode {
			continue
		}
		switch answer := source.Get(key); {
		case answer != nil:
			running[key] = answer
		case !leftToTheirReader[key]:
			unanswered = append(unanswered, key)
		}
	}
	if len(unanswered) > 0 {
		sort.Strings(unanswered)
		return nil, fmt.Errorf("app.toml and the start command's flags do not answer %v, so each runs its "+
			"reader's own default, which a file leaving it out would replace with the declared value. "+
			"State them in app.toml and run this again", unanswered)
	}
	return running, nil
}

// theHomeHoldsANodeToDescribe refuses a home without config.toml, whose values would all be this
// binary's rather than a node's. app.toml is not required, since a boot generates it.
func theHomeHoldsANodeToDescribe(home string) error {
	path := filepath.Join(home, "config", "config.toml")
	switch _, err := os.Stat(path); {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s is not there, so this home holds no node to describe and every value "+
			"written would come from this binary rather than from anything a node runs", path)
	case err != nil:
		return err
	}
	return nil
}

// noSeiTomlIsInUse refuses a home that already holds a sei.toml. A node under this manager runs what that
// file says, so its legacy files no longer describe it.
func noSeiTomlIsInUse(home string) error {
	path := filepath.Join(home, "config", seiTomlName)
	switch _, err := os.Lstat(path); {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	}
	return fmt.Errorf("%s already exists, and a node reading it does not run what app.toml and "+
		"config.toml say, so --%s would not describe this node", path, flagGenerateFromLegacy)
}

// theKindTheNodesOwnFileRecords applies theKindThisNodeAlreadyRuns when the home holds a config.toml.
func theKindTheNodesOwnFileRecords(home string, mode registry.Mode) error {
	switch _, err := os.Stat(filepath.Join(home, "config", "config.toml")); {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	}
	own, err := theNodesOwnConfiguration(home)
	if err != nil {
		return fmt.Errorf("read this node's own configuration: %w", err)
	}
	return theKindThisNodeAlreadyRuns(mode, own)
}

// theKindThisNodeAlreadyRuns refuses a mode that disagrees with the one config.toml records, since a boot
// delivers nothing from such a file. Archive pairs with full, which has no separate name there.
func theKindThisNodeAlreadyRuns(mode registry.Mode, own *tmcfg.Config) error {
	if own == nil || !modesDisagree(string(mode), own.Mode) {
		return nil
	}
	return fmt.Errorf("this node's own configuration file records it running as %q and --%s says %q. A "+
		"boot delivers nothing from a file that disagrees, so the file written here would leave every "+
		"declared key reading as it does now", own.Mode, flagGenerateMode, mode)
}

// whatTheDeclarationDoesNotAlreadySay returns the keys this node answers differently from mode's defaults,
// with the node's own values. Compared as text, since the two sides often hold different Go types.
func whatTheDeclarationDoesNotAlreadySay(mode registry.Mode, running map[string]any) (map[string]any, error) {
	resolved, err := registry.Resolve(mode, registry.Sources{})
	if err != nil {
		return nil, fmt.Errorf("resolve this binary's defaults for a %s node: %w", mode, err)
	}
	stated := map[string]any{}
	for key, declared := range resolved.Values {
		answer, answers := running[key]
		if !answers {
			continue
		}
		if fmt.Sprint(answer) == fmt.Sprint(declared) {
			continue
		}
		stated[key] = answer
	}
	return stated, nil
}

// theFileStating returns a document with the schema version, node mode, and stated keys in sorted order,
// so two runs over one node produce the same bytes.
func theFileStating(mode registry.Mode, stated map[string]any) (*seitoml.File, error) {
	file, err := seitoml.New(string(mode))
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(stated))
	for key := range stated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := file.Set(key, stated[key]); err != nil {
			return nil, fmt.Errorf("state %s: %w", key, err)
		}
	}
	return file, nil
}

// thisFilePlacedOrPrinted prints the file, or with --write places it where a boot reads it, never
// replacing an existing one.
func thisFilePlacedOrPrinted(cmd *cobra.Command, home string, file *seitoml.File, stated int) error {
	write, err := cmd.Flags().GetBool(flagGenerateWrite)
	if err != nil {
		return err
	}
	if !write {
		body, err := file.Bytes()
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(body)
		return err
	}

	dir := filepath.Join(home, "config")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(dir, seiTomlName)
	switch err := file.SaveNew(path); {
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s already exists and is left alone. Print this instead and compare them", path)
	case err != nil:
		return err
	}
	report(cmd.OutOrStdout(), fmt.Sprintf("wrote %s, stating %d of this binary's %d declared keys",
		path, stated, len(registry.Keys())))
	return nil
}
