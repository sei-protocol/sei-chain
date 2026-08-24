package configmanager

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// CheckCmd answers, without starting a node, whether this binary can use a sei.toml.
//
// A boot may not refuse a file. A node that stopped because one line was mistyped is worse than a node
// running the value it ran yesterday, so every failure at boot is a report and the node keeps going. That
// makes the report the only signal, and a fleet rolling a configuration change forward reads it after the
// change is already on every node.
//
// The same questions have exact answers before then. The file, the binary and the environment are all the
// input, so a refusal is deterministic: the same file against the same binary gives the same answer here as
// it will at boot. This asks them where an answer costs a failed check rather than a restart.
func CheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report whether this binary can use the node's sei.toml",
		Long: "Resolves the node's sei.toml the way a boot resolves it and reports every value this " +
			"binary would refuse, without starting anything. Exits non-zero if there is one.\n\n" +
			"A boot cannot refuse a file, so it applies what it can and reports the rest. Running this " +
			"first is how a mistyped value costs a failed check rather than a restart.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			problems, found, err := checkSeiToml(cmd)
			if err != nil {
				return err
			}
			if !found {
				report(cmd.OutOrStdout(), "this node has no sei.toml, so every key reads as it always "+
					"has and there is nothing here to be wrong")
				return nil
			}
			out := cmd.OutOrStdout()
			for _, line := range problems {
				report(out, line)
			}
			if len(problems) > 0 {
				return fmt.Errorf("%d problem(s); a boot would apply what it could and report the rest",
					len(problems))
			}
			report(out, "every value this file supplies is one this binary can use")
			return nil
		},
	}
	return cmd
}

// report writes one line of the answer.
//
// A failed write is dropped rather than returned. Where this runs the answer is the exit status, and a
// caller that cannot read the report still gets that.
func report(out io.Writer, line string) { _, _ = fmt.Fprintln(out, line) }

// checkSeiToml resolves the node's file and returns what a boot would refuse, in the order it would.
//
// The absence of a file is not a problem to report. A node without one reads exactly as it always has, so
// there is nothing here that could be wrong.
func checkSeiToml(cmd *cobra.Command) (problems []string, found bool, err error) {
	home, err := resolveHomeDir(cmd)
	if err != nil {
		return nil, false, fmt.Errorf("resolve the home directory: %w", err)
	}
	file, ok := readSeiTomlAt(home)
	if !ok {
		return nil, false, nil
	}
	mode, err := file.Mode()
	if err != nil {
		return []string{fmt.Sprintf("sei.toml records no usable node mode: %v", err)}, true, nil
	}
	written, err := file.Values()
	if err != nil {
		return []string{fmt.Sprintf("sei.toml cannot be read: %v", err)}, true, nil
	}

	resolved, err := registry.Resolve(registry.Mode(mode), registry.Sources{
		File:      written,
		LookupEnv: os.LookupEnv,
		Flags:     flagValues(TypedFlags(cmd)),
	})
	if err != nil {
		return []string{fmt.Sprintf("this node's configuration cannot be resolved: %v", err)}, true, nil
	}

	for _, key := range resolved.Unknown {
		problems = append(problems, fmt.Sprintf("%s: sei.toml writes this and no section declares it, "+
			"so it has no effect", key))
	}
	problems = append(problems, whatADecodeWouldRefuse(resolved)...)
	return problems, true, nil
}

// whatADecodeWouldRefuse rehearses each decoded section the way the boot's delivery does.
//
// Rehearsed against a fresh configuration rather than a running node's, because there is no node here. That
// is a weaker target than the delivery uses, and the difference is the point: a value this accepts may still
// be refused at boot if the field it lands on holds something this cannot see. It is why this reports what
// it can answer and the boot still reports what it finds.
func whatADecodeWouldRefuse(resolved registry.Resolved) []string {
	bySection := registry.SuppliedByDecodedSection(resolved)
	var problems []string
	for _, name := range sortedSectionNames(bySection) {
		values := bySection[name]
		base := tmcfg.DefaultConfig()

		if bad := refuseWhatDecodesToSomethingElse(base, values); len(bad) > 0 {
			problems = append(problems, fmt.Sprintf("[%s]: %s is a length of time written as a plain "+
				"number, which reads as nanoseconds", name, strings.Join(bad, "; ")))
			continue
		}

		source := viper.New()
		for key, value := range values {
			source.Set(key, value)
		}
		candidate, err := copyNodeConfig(base)
		if err != nil {
			problems = append(problems, fmt.Sprintf("[%s]: cannot be rehearsed: %v", name, err))
			continue
		}
		if err := source.Unmarshal(candidate); err != nil {
			problems = append(problems, fmt.Sprintf("[%s]: %v, so none of this section would apply "+
				"(keys: %s)", name, err, strings.Join(sortedKeys(values), ",")))
		}
	}
	sort.Strings(problems)
	return problems
}
