package configmanager

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// CheckCmd returns `seid config check`, which reports, without starting a node, what a boot would refuse
// in its sei.toml. It reads the environment of whoever runs it, which may differ from the node's. It does
// not rehearse appopts.Install, whose target only a boot builds.
func CheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report whether this binary can use the node's sei.toml",
		Long: "Resolves the node's sei.toml the way a boot resolves it and reports every value this " +
			"binary would refuse, without starting anything. Exits non-zero if there is one.\n\n" +
			"A boot cannot refuse a file, so it applies what it can and reports the rest. Running this " +
			"first is how a mistyped value costs a failed check rather than a restart.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		// Replaces the root hook, which would generate missing config files and mark flags changed from
		// app.toml values.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// First, and regardless of the file: an invalid gate value stops the boot before it.
			problems, notes := whatTheGateSays(os.Getenv)

			inTheFile, fromTheFile, found, err := checkSeiToml(cmd)
			if err != nil {
				return err
			}
			problems = append(problems, inTheFile...)
			notes = append(notes, fromTheFile...)

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

// whatTheGateSays reports an invalid SEI_CONFIG_MANAGER as a problem, since a boot would refuse it, and
// a gate that is not v2 as a note, since a boot would then ignore sei.toml.
func whatTheGateSays(getenv func(string) string) (problems, notes []string) {
	mgr, err := Select(getenv)
	if err != nil {
		return []string{fmt.Sprintf("%s is set to something this binary does not accept, so a boot "+
			"would refuse before reaching this file: %v", EnvVar, err)}, nil
	}
	// Asked of the manager, since Select owns which values read the file.
	if _, reads := mgr.(SeiConfigManager); !reads {
		return nil, []string{fmt.Sprintf("%s is not set to v2 for this command, so a boot in the same "+
			"environment reads none of this file. What follows is what it would reach if it were", EnvVar)}
	}
	return nil, nil
}

// report writes one line of the answer, ignoring a write error since the exit status is the answer.
func report(out io.Writer, line string) { _, _ = fmt.Fprintln(out, line) }

// checkSeiToml resolves the node's sei.toml and returns what a boot would refuse, in the order it would.
// A missing file is not a problem; an unreadable one is.
func checkSeiToml(cmd *cobra.Command) (problems, notes []string, found bool, err error) {
	home, err := resolveHomeDir(cmd)
	if err != nil {
		return nil, nil, false, fmt.Errorf("resolve the home directory: %w", err)
	}
	// An empty home would read ./config, which may be another node's.
	if home == "" {
		return nil, nil, false, fmt.Errorf("no home directory is set, so there is no sei.toml to check. "+
			"Pass --home, or set %s", theVariableThatSetsTheHome())
	}
	file, err := readSeiTomlAt(home)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, false, nil
	case err != nil:
		return []string{fmt.Sprintf("sei.toml cannot be read: %v", err)}, nil, true, nil
	}
	mode, err := file.Mode()
	if err != nil {
		return []string{fmt.Sprintf("sei.toml records no usable node mode: %v", err)}, nil, true, nil
	}
	written, err := file.Values()
	if err != nil {
		return []string{fmt.Sprintf("sei.toml cannot be read: %v", err)}, nil, true, nil
	}

	resolved, err := registry.Resolve(registry.Mode(mode), registry.Sources{
		File:      written,
		LookupEnv: os.LookupEnv,
		Flags:     flagValues(TypedFlags(cmd)),
	})
	if err != nil {
		return []string{fmt.Sprintf("this node's configuration cannot be resolved: %v", err)}, nil, true, nil
	}

	// A refused registration makes its keys look unknown, so report it first.
	problems = append(problems, whatTheResolutionAlreadyKnows(resolved)...)

	// Unknown flags are expected and not reported.
	for _, key := range resolved.UnknownInFile {
		problems = append(problems, fmt.Sprintf("%s: sei.toml writes this and no section declares it, "+
			"so it has no effect", key))
	}
	own, err := theNodesOwnConfiguration(home)
	running := ""
	if own != nil {
		running = own.Mode
	}
	switch {
	case err != nil:
		problems = append(problems, fmt.Sprintf("the node's own configuration file cannot be read, so a "+
			"boot would not start: %v", err))
	case modesDisagree(mode, running):
		problems = append(problems, fmt.Sprintf("sei.toml says this is a %s and the node's own "+
			"configuration file says %s. Every value resolved here is the answer for the first and the "+
			"node would run as the second", mode, running))
	}
	problems = append(problems, whatADecodeWouldRefuse(resolved, own)...)
	notes = append(notes, whatTheFileLeavesToTheDeclaration(resolved, written, mode))
	return problems, notes, true, nil
}

// theVariableThatSetsTheHome names the environment variable the home resolves from, derived as
// resolveHomeDir derives it.
func theVariableThatSetsTheHome() string {
	exe, err := os.Executable()
	if err != nil {
		return "the home variable for this binary"
	}
	return strings.ToUpper(path.Base(exe)) + "_HOME"
}

// theNodesOwnConfiguration decodes config.toml over the defaults, as a boot does. A missing file yields
// the defaults; any other failure is an error.
func theNodesOwnConfiguration(home string) (*tmcfg.Config, error) {
	cfg := tmcfg.DefaultConfig()
	v := viper.New()
	v.SetConfigFile(filepath.Join(home, "config", "config.toml"))
	switch err := v.ReadInConfig(); {
	case errors.Is(err, fs.ErrNotExist):
		return cfg, nil
	case err != nil:
		return nil, err
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// whatTheFileLeavesToTheDeclaration counts the declared keys the file states, those another source
// answers, and those left to the binary's default.
func whatTheFileLeavesToTheDeclaration(resolved registry.Resolved, written map[string]any,
	mode string) string {
	inTheFile := make(map[string]bool, len(written))
	for key := range written {
		inTheFile[strings.ToLower(key)] = true
	}
	stated := 0
	for key := range resolved.Values {
		if inTheFile[strings.ToLower(key)] {
			stated++
		}
	}
	// Overrides includes the file's keys, so the remainder took the declared default.
	declared := len(resolved.Values) - len(resolved.Overrides)
	elsewhere := len(resolved.Overrides) - stated

	line := fmt.Sprintf("this file states %d of %d declared keys", stated, len(resolved.Values))
	if elsewhere > 0 {
		line += fmt.Sprintf("; %d more are answered by an environment variable or a flag", elsewhere)
	}
	return line + fmt.Sprintf("; the other %d take the value this binary declares for a %s, whatever "+
		"app.toml and config.toml currently say", declared, mode)
}

// whatTheResolutionAlreadyKnows returns the resolution's refused registrations and ignored environment
// variables as problems.
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

// whatADecodeWouldRefuse rehearses each decoded section against the node's own configuration, the way
// deliverOneSection does.
func whatADecodeWouldRefuse(resolved registry.Resolved, own *tmcfg.Config) []string {
	bySection, _ := registry.ResolvedAndOwnedByDecodedSections(resolved)
	base := own
	if base == nil {
		base = tmcfg.DefaultConfig()
	}
	fields := keyFieldTypes(reflect.TypeOf(*base), "")

	var problems []string
	for _, name := range sortedKeys(bySection) {
		values := bySection[name]
		keys := sortedKeys(values)

		// Before the decode, which accepts these silently.
		if bad := whatDecodesToSomethingElse(fields, values); len(bad) > 0 {
			problems = append(problems, fmt.Sprintf("[%s]: %s", name,
				strings.Join(problemsInOrder(bad), "; ")))
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
				"(keys: %s)", name, err, strings.Join(keys, ",")))
			continue
		}

		if err := whatTheSectionsOwnRulesSay(candidate, keys); err != nil {
			problems = append(problems, fmt.Sprintf("[%s]: %v, so none of this section would apply "+
				"(keys: %s)", name, err, strings.Join(keys, ",")))
		}
	}
	sort.Strings(problems)
	return problems
}
