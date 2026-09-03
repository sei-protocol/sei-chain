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

// flagGenerateMode names the kind of node the written values resolve for.
//
// The same name the provisioning command takes, spelled here rather than shared, because the package
// holding that command imports this one and cannot be imported back.
const flagGenerateMode = "mode"

// flagGenerateWrite places the file instead of printing it.
const flagGenerateWrite = "write"

// GenerateCmd writes a sei.toml stating what this node already runs.
//
// A node under this manager answers every declared key from its sei.toml, so a sparse file is a large
// change to a node whose own files were tuned by hand: every declared key the file leaves out moves to the
// value this binary declares, and there are more than two hundred. Writing that file by hand means finding
// each of those by reading two files against a binary's defaults.
//
// This writes it instead, from what the node answers today. A key it states is a key this node answers
// differently from the declaration, so a node started against the written file runs what it ran before.
//
// Two things make the written file run what the node ran, and the divergence filter is neither of them.
// Every value it states is the node's own, read where that setting's reader reads it, so a stated key
// arrives as what the node already held. And a key nothing answers is not stated, because there is no
// value to state: its reader holds a default of its own and stating this binary's declaration instead is
// the one way a writer here changes a setting nobody decided to change.
//
// What the filter does is leave out a key whose answer is already the declared value. Those lines would
// state what the declaration states, so the file is shorter and every line in it marks a decision.
//
// What it does not read is the environment. A variable that answers a declared key goes on answering it
// after this runs, at the same precedence, so baking one into a file would state a value twice and leave
// the copy in the file with no effect.
func GenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Write a sei.toml stating what this node already runs",
		Long: "Reads this node's app.toml and config.toml the way a boot reads them, and writes the " +
			"sei.toml that makes a node under this manager run what this one runs today.\n\n" +
			"Prints the file by default. Pass --write to place it in the node's config directory.",
		Args: cobra.NoArgs,
		// Nothing here is a usage error once the flags parse, and the production wiring does not silence
		// usage, so cobra would print the whole usage block after the error.
		SilenceUsage: true,
		// A hook of its own, which stops the root one from running. Cobra runs the closest hook it finds.
		// The root hook runs the boot's configuration handler, which generates config.toml and app.toml
		// when they are absent and copies configuration values into flags. This command reads those files
		// to answer what a node runs, so it must not be the thing that creates them, and it must read the
		// flags before values are copied into them.
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
			if err := theHomeHoldsANodeToDescribe(home); err != nil {
				return err
			}
			own, err := theNodesOwnConfiguration(home)
			if err != nil {
				return fmt.Errorf("read this node's own configuration: %w", err)
			}
			if err := theKindThisNodeAlreadyRuns(mode, own); err != nil {
				return err
			}
			running, err := whatThisNodeAlreadyRuns(cmd, home, own)
			if err != nil {
				return err
			}
			stated, err := whatTheDeclarationDoesNotAlreadySay(mode, running)
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
	cmd.Flags().Bool(flagGenerateWrite, false,
		"place the file at config/"+seiTomlName+" instead of printing it")
	return cmd
}

// theModeTheWrittenValuesResolveFor reads the kind of node this file is for.
//
// Required rather than guessed. Every value in the file resolves for one kind, and the kinds differ on
// keys that decide whether a node prunes and whether it serves queries. A guess that lands on the wrong
// one writes a file whose values were chosen against defaults the node does not use.
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

// whatThisNodeAlreadyRuns reads the value this node answers for every declared key it answers at all.
//
// Both deliveries, read the way each is delivered. The keys a decode delivers are read off the struct
// their file is decoded into, because that is where their reader looks. Every other key is read off the
// source a lookup reads, which is app.toml over the start command's flag defaults.
//
// A key nothing answers is absent from the result rather than present and empty. Its reader holds a
// default of its own, and a caller cannot tell an unanswered key from one answered with a zero.
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
	for _, key := range registry.Keys() {
		if _, byADecode := decoded[key]; byADecode {
			continue
		}
		if answer := source.Get(key); answer != nil {
			running[key] = answer
		}
	}
	return running, nil
}

// theHomeHoldsANodeToDescribe refuses a home that no node has been created in.
//
// Both files are absent in that case, so every value would come from a flag's default and the file
// written would describe this binary rather than a node. It would still look like a node's
// configuration, and the kind recorded in it would be whichever kind was asked for.
//
// The node's own configuration file is the one checked, because a boot generates the other one and a
// home can legitimately hold only the first.
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

// theKindThisNodeAlreadyRuns refuses a kind of node that disagrees with the one this node's own
// configuration file records.
//
// A boot delivers nothing at all from a file whose kind disagrees with the kind the node runs as, so a
// file written under the wrong kind is not a partly-right file. Every declared key goes on reading as it
// did, and an operator holds a file they have every reason to believe is in use. Refused here, where it
// costs a message.
//
// One pairing is not a disagreement, and the shared answer is used rather than a second copy of the rule:
// the kind that keeps every version of history has no name in the node's own file.
func theKindThisNodeAlreadyRuns(mode registry.Mode, own *tmcfg.Config) error {
	if own == nil || !modesDisagree(string(mode), own.Mode) {
		return nil
	}
	return fmt.Errorf("this node's own configuration file records it running as %q and --%s says %q. A "+
		"boot delivers nothing from a file that disagrees, so the file written here would leave every "+
		"declared key reading as it does now", own.Mode, flagGenerateMode, mode)
}

// whatTheDeclarationDoesNotAlreadySay returns the keys this node answers differently from the declaration.
//
// Compared as text, because the two sides carry different Go types for the same key often enough that
// comparing values would be comparing shapes. What is written is the node's own value, with the type it
// holds, so it reaches its setting as what it says.
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

// theFileStating returns a document carrying this binary's schema version, the kind of node, and one line
// per key.
//
// Keys written in order, so two runs over one node produce the same bytes and a difference between two
// files is a difference in what they state.
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

// thisFilePlacedOrPrinted prints the file, or puts it where a boot reads it.
//
// Printing is the default. This renders a file a boot reads for every setting a node has, so an operator
// reading it before it takes effect is the ordinary case, and `>` puts it where they want it.
//
// An existing file is never replaced. It holds what somebody decided, and the comments beside those
// decisions, and this command cannot tell a file it wrote last week from one edited since.
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

	// Asked before the document is rendered, so a home that already holds a file costs nothing and the
	// refusal is the same whatever the file would have said.
	path := filepath.Join(home, "config", seiTomlName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists and states what somebody decided, so it is left alone. "+
			"Print this instead and compare them", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := file.Save(path); err != nil {
		return err
	}
	report(cmd.OutOrStdout(), fmt.Sprintf("wrote %s, stating %d of this node's %d declared keys",
		path, stated, len(registry.Keys())))
	return nil
}
