package configmanager

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
// input, so the same file against the same binary gives the same answer here as it will at boot, for the
// same environment. That last part is a real condition and not a formality: this reads the environment of
// whoever runs it, and a node started by an init system or a container runtime has a different one. A
// variable that answers a declared key is a variable this cannot see unless it is set here too.
//
// What it does not rehearse is the install into the source a node builds, because that source does not
// exist until a boot builds it. A key can be refused there for a reason nothing here can see, and the whole
// install is dropped when it is. That is worth adding when the surface it covers is more than a handful of
// keys on a live node.
//
// This asks what it can answer where an answer costs a failed check rather than a restart.
func CheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report whether this binary can use the node's sei.toml",
		Long: "Resolves the node's sei.toml the way a boot resolves it and reports every value this " +
			"binary would refuse, without starting anything. Exits non-zero if there is one.\n\n" +
			"A boot cannot refuse a file, so it applies what it can and reports the rest. Running this " +
			"first is how a mistyped value costs a failed check rather than a restart.",
		Args: cobra.NoArgs,
		// A mistyped value is not a usage error, and nothing in the production wiring silences usage, so
		// cobra would follow this command's error with the whole usage block into the same stdout the
		// report was just written to.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// The gate is answered first and whether or not there is a file, because a value this binary
			// refuses stops the node before it reaches one. Reported as a problem for the same reason: the
			// exit status is this command's answer, and a node that cannot start is not a pass.
			problems, notes := whatTheGateSays(os.Getenv)

			inTheFile, found, err := checkSeiToml(cmd)
			if err != nil {
				return err
			}
			problems = append(problems, inTheFile...)

			switch {
			case !found:
				notes = append(notes, "this node has no sei.toml, so every key reads as it always has "+
					"and there is nothing in it to be wrong")
			case len(problems) == 0:
				notes = append(notes, "every value this file supplies is one this binary can use")
			}

			for _, line := range append(notes, problems...) {
				report(out, line)
			}
			if len(problems) > 0 {
				return fmt.Errorf("%d problem(s); a boot would apply what it could and report the rest",
					len(problems))
			}
			return nil
		},
	}
	return cmd
}

// whatTheGateSays answers what the gate means for this node, split into problems and notes.
//
// A value this binary does not accept is a problem rather than a note. A boot refuses on it before reaching
// any file, so the node will not start, and this command's answer is its exit status: reporting that as a
// passing run tells a runbook the node is fine when it cannot boot.
//
// A gate that is simply off is a note. Without it a passing check reads as "this file is in use and
// correct" on a node where a boot ignores the file completely, which is every node until an operator
// switches it. That is the wrong conclusion in the more dangerous direction, because it invites somebody to
// trust a file nothing reads.
//
// Answered from the environment this command runs in, which is the same limitation the resolution has.
func whatTheGateSays(getenv func(string) string) (problems, notes []string) {
	if _, err := Select(getenv); err != nil {
		return []string{fmt.Sprintf("%s is set to something this binary does not accept, so a boot "+
			"would refuse before reaching this file: %v", EnvVar, err)}, nil
	}
	if getenv(EnvVar) != "v2" {
		return nil, []string{fmt.Sprintf("%s is not set to v2 for this command, so a boot in the same "+
			"environment reads none of this file. What follows is what it would reach if it were", EnvVar)}
	}
	return nil, nil
}

// report writes one line of the answer.
//
// A failed write is dropped rather than returned. Where this runs the answer is the exit status, and a
// caller that cannot read the report still gets that.
func report(out io.Writer, line string) { _, _ = fmt.Fprintln(out, line) }

// checkSeiToml resolves the node's file and returns what a boot would refuse, in the order it would.
//
// The absence of a file is not a problem to report: a node without one reads exactly as it always has, so
// there is nothing here that could be wrong. A file that exists and will not read is the opposite, and is
// reported as a problem of a file that was found.
func checkSeiToml(cmd *cobra.Command) (problems []string, found bool, err error) {
	home, err := resolveHomeDir(cmd)
	if err != nil {
		return nil, false, fmt.Errorf("resolve the home directory: %w", err)
	}
	file, err := readSeiTomlAt(home)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// The absence of a file is the one case with nothing to report.
		return nil, false, nil
	case err != nil:
		// A file that exists and will not read is the case this command exists for. Reported as a problem
		// of a file that was found, so the command exits non-zero: an operator running this before a
		// restart is asking whether their file is right, and answering that they have no file is both
		// wrong and the answer least likely to make them look.
		return []string{fmt.Sprintf("sei.toml cannot be read: %v", err)}, true, nil
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

	// First, because a refused registration is what makes the report below name the wrong file: the
	// section's keys are absent from the declared set, so an operator's valid key for one of them reads as
	// a key nothing declares. Whoever reads this output in order has to meet the cause first.
	problems = append(problems, whatTheResolutionAlreadyKnows(resolved)...)

	// Only the file's own keys. A flag matching no declared key arrives in the same resolution and is not
	// a mistake: every command carries flags that name no setting, so reporting those would fail this
	// check on every invocation that types one, including a correct file.
	for _, key := range resolved.UnknownInFile {
		problems = append(problems, fmt.Sprintf("%s: sei.toml writes this and no section declares it, "+
			"so it has no effect", key))
	}
	running, err := theModeTheNodesOwnFileRecords(home)
	switch {
	case err != nil:
		problems = append(problems, fmt.Sprintf("the node's own configuration file cannot be read, so a "+
			"boot would not start: %v", err))
	case modesDisagree(mode, running):
		problems = append(problems, fmt.Sprintf("sei.toml says this is a %s and the node's own "+
			"configuration file says %s. Every value resolved here is the answer for the first and the "+
			"node would run as the second", mode, running))
	}
	problems = append(problems, whatADecodeWouldRefuse(resolved)...)
	return problems, true, nil
}

// theModeTheNodesOwnFileRecords reads what kind of node the node's own configuration file says this is.
//
// Read from the file here rather than taken from a running node, because nothing is running.
//
// An absent file, or one with no mode in it, answers what a boot would compute rather than empty. A boot
// starts from this binary's defaults and writes the file itself when it is missing, so the running mode is
// never empty on that path. Answering empty here would pass a node whose sei.toml names a different kind,
// and then that node reports the disagreement at its loudest level on the next start: a pass in exactly
// the case with the largest consequence.
func theModeTheNodesOwnFileRecords(home string) (string, error) {
	v := viper.New()
	v.SetConfigFile(filepath.Join(home, "config", "config.toml"))
	switch err := v.ReadInConfig(); {
	case errors.Is(err, fs.ErrNotExist):
		// The only case that is an absence. Every other failure is a file somebody wrote that this
		// binary cannot read, and a boot does not start at all on one of those, so answering with a
		// default would pass a node that cannot boot.
	case err != nil:
		return "", err
	}
	// A key that is present answers with what it holds, empty included. A boot unmarshals that empty
	// string over the default, so the running kind really is empty and the comparison beside this reports
	// it. Substituting the default here would pass a node the boot goes on to report.
	if v.IsSet("mode") {
		return v.GetString("mode"), nil
	}
	return tmcfg.DefaultConfig().Mode, nil
}

// whatTheResolutionAlreadyKnows returns the problems a resolution reports without any rehearsal.
//
// Two of them, and the boot reports both. A refused registration leaves its section's keys out of the
// declared set, so an operator's valid key lands among the undeclared ones and this command would tell them
// their file is wrong about a key it was right about. Reported first for that reason, so whoever reads the
// output in order meets the cause before the symptom.
//
// The other is a variable set for a key the environment cannot carry. The boot warns about it and this
// command was silent, so an operator was not told that the channel they reached for did nothing.
func whatTheResolutionAlreadyKnows(resolved registry.Resolved) []string {
	problems := make([]string, 0, len(resolved.Refused)+len(resolved.Ignored))
	for _, d := range resolved.Refused {
		problems = append(problems, fmt.Sprintf("this binary's own registration of [%s] carries a defect, "+
			"so some declared keys do not reach the node as declared: %v", d.Section, d.Err))
	}
	for _, key := range sortedKeys(resolved.Ignored) {
		problems = append(problems, fmt.Sprintf("%s: an environment variable is set for this and the "+
			"environment cannot supply it, so it has no effect and the file's value applies (%s)",
			key, resolved.Ignored[key]))
	}
	return problems
}

// whatADecodeWouldRefuse rehearses each decoded section the way the boot's delivery does.
//
// Rehearsed against a fresh configuration rather than a running node's, because there is no node here. That
// is a weaker target than the delivery uses, and the difference is the point: a value this accepts may still
// be refused at boot if the field it lands on holds something this cannot see. It is why this reports what
// it can answer and the boot still reports what it finds.
func whatADecodeWouldRefuse(resolved registry.Resolved) []string {
	bySection, _ := registry.SuppliedAndOwnedByDecodedSections(resolved)
	var problems []string
	for _, name := range sortedKeys(bySection) {
		values := bySection[name]
		base := tmcfg.DefaultConfig()

		// Each message says what is wrong with the value it names, and there is more than one thing that
		// can be. Stating one of them here would describe the others wrongly.
		if bad := whatDecodesToSomethingElse(base, values); len(bad) > 0 {
			problems = append(problems, fmt.Sprintf("[%s]: %s", name, strings.Join(bad, "; ")))
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
			continue
		}

		// The node's own rules, the same step the delivery takes after a clean decode. Without it this
		// command passes a file the boot drops, which is the divergence it exists to prevent.
		if err := candidate.ValidateBasic(); err != nil {
			problems = append(problems, fmt.Sprintf("[%s]: %v, so none of this section would apply "+
				"(keys: %s)", name, err, strings.Join(sortedKeys(values), ",")))
		}
	}
	sort.Strings(problems)
	return problems
}
