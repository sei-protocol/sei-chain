package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	svrcmd "github.com/sei-protocol/sei-chain/sei-cosmos/server/cmd"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// TestANodeStartedFromTheGeneratedFileRunsWhatItRanBefore is what the command is for.
//
// A node under this manager answers every declared key from its sei.toml, so a file that states too
// little moves settings to their declared value. That is the whole risk of handing an operator a
// generated file, and it is one measurement: read what the node runs, write the file, start the node
// against it, and read again.
//
// Both deliveries, because they are read in different places and a file can be right about one of them.
func TestANodeStartedFromTheGeneratedFileRunsWhatItRanBefore(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	if err := os.MkdirAll(filepath.Join(home.Root, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	before := whatTheNodeAnswers(t, home.Root)

	out, err := runGenerateThroughRoot(t, home.Root, "--mode", "validator")
	if err != nil {
		t.Fatalf("generate was refused: %v\n%s", err, out)
	}
	seiToml := filepath.Join(home.Root, "config", "sei.toml")
	if err := os.WriteFile(seiToml, []byte(out), 0o600); err != nil {
		t.Fatalf("write %s: %v", seiToml, err)
	}

	after := whatTheNodeAnswers(t, home.Root)

	var moved, lost []string
	for key, was := range before.lookup {
		now, answers := after.lookup[key]
		switch {
		case !answers:
			lost = append(lost, key)
		case fmt.Sprint(was) != fmt.Sprint(now):
			moved = append(moved, fmt.Sprintf("%s was %v and is %v", key, was, now))
		}
	}
	sort.Strings(moved)
	sort.Strings(lost)
	for _, line := range moved {
		t.Errorf("a key a lookup delivers moved: %s. The file this command wrote does not state what the "+
			"node ran, so an operator adopting it changes a setting they did not decide to change", line)
	}
	for _, key := range lost {
		t.Errorf("%s was answered before the file and is answered by nothing after it, so its reader now "+
			"holds a default of its own where the node had a value", key)
	}

	for key, was := range before.decoded {
		if now := after.decoded[key]; was != now {
			t.Errorf("a key a decode delivers moved: %s was %q and is %q", key, was, now)
		}
	}

	if !t.Failed() {
		t.Logf("%d keys a lookup answers and %d a decode delivers, all unchanged across the file",
			len(before.lookup), len(before.decoded))
	}
}

// TestTheGeneratedFileStatesTheKeysThatDivergeAndNoOthers holds which keys the command writes.
//
// A file stating fewer keys than these moves a setting, and the test above catches that by starting a
// node. This catches the other direction, which that one cannot: a line stating what the declaration
// already states changes nothing, so a file full of them passes a round trip while telling an operator
// that two hundred settings were decisions.
//
// Measured against the record for every kind of node, because the command derives the set from the files
// and the record derives it from a boot.
func TestTheGeneratedFileStatesTheKeysThatDivergeAndNoOthers(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	if err := os.MkdirAll(filepath.Join(home.Root, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	whatTheNodeAnswers(t, home.Root)

	for _, mode := range registry.Modes() {
		t.Run(string(mode), func(t *testing.T) {
			out, err := runGenerateThroughRoot(t, home.Root, "--mode", string(mode))
			if err != nil {
				t.Fatalf("generate was refused: %v\n%s", err, out)
			}
			stated := whatTheFileStates(t, out)

			want := divergences[mode]

			for key := range stated {
				if _, records := want[key]; !records {
					t.Errorf("the file states %s and nothing records it as diverging, so either it "+
						"states a key that changes nothing or a divergence is unrecorded", key)
				}
			}
			for key := range want {
				if _, states := stated[key]; !states {
					t.Errorf("%s is recorded as diverging and the file does not state it, so a node "+
						"adopting the file moves that setting to its declared value", key)
				}
			}
		})
	}
}

// nodeAnswers is what a node holds, read the way each delivery is read.
type nodeAnswers struct {
	lookup  map[string]any
	decoded map[string]string
}

// whatTheNodeAnswers starts a node in this home and reads every declared key off it.
//
// Booted twice for the reason the agreement measurement is: the first writes the files, and a flag bound
// to a key the writer sets overwrites that value before anything reads it back.
func whatTheNodeAnswers(t *testing.T, home string) nodeAnswers {
	t.Helper()
	var ctx *server.Context
	for boot := 0; boot < 2; boot++ {
		cmd := server.StartCmd(nil, home, []trace.TracerProviderOption{})
		if err := cmd.Flags().Set("home", home); err != nil {
			t.Fatalf("set --home: %v", err)
		}
		got, err := runManager(t, configmanager.SeiConfigManager{}, cmd)
		if err != nil {
			t.Fatalf("boot %d was refused: %v", boot+1, err)
		}
		ctx = got
	}

	decoded := map[string]bool{}
	for _, key := range keysADecodeDelivers() {
		decoded[key] = true
	}
	lookup := map[string]any{}
	for _, key := range registry.Keys() {
		if decoded[key] {
			continue
		}
		if answer := ctx.Viper.Get(key); answer != nil {
			lookup[key] = answer
		}
	}
	return nodeAnswers{
		lookup:  lookup,
		decoded: configmanager.DescribeForTest(t, ctx.Config, keysADecodeDelivers()),
	}
}

// whatTheFileStates returns the keys a rendered sei.toml states, leaving out the two the file itself
// carries about its own schema and the kind of node.
func whatTheFileStates(t *testing.T, body string) map[string]any {
	t.Helper()
	file, err := seitoml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("the command rendered a file this binary cannot read: %v\n%s", err, body)
	}
	values, err := file.Values()
	if err != nil {
		t.Fatalf("read the rendered file's values: %v", err)
	}
	return values
}

// runGenerateThroughRoot runs `config generate` the way an operator runs it.
//
// Through the real root command, because the command reads the start command's flags off the root to
// answer what a node runs. Built on its own it has no siblings and would refuse.
func runGenerateThroughRoot(t *testing.T, home string, extraArgs ...string) (string, error) {
	t.Helper()
	root, _ := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs(append([]string{"config", "generate", "--home", home}, extraArgs...))
	err := svrcmd.Execute(root, home)
	return out.String(), err
}

// TestGenerateWritesWhereABootReadsIt covers the flag that places the file.
//
// Printing is the default and a printed file is not in use, so the one thing that makes the command
// finish an operator's task is the file landing where the boot looks for it.
func TestGenerateWritesWhereABootReadsIt(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	if err := os.MkdirAll(filepath.Join(home.Root, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	before := whatTheNodeAnswers(t, home.Root)

	out, err := runGenerateThroughRoot(t, home.Root, "--mode", "validator", "--write")
	if err != nil {
		t.Fatalf("generate was refused: %v\n%s", err, out)
	}

	path := filepath.Join(home.Root, "config", "sei.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the command reported writing a file and %s is not there: %v\n%s", path, err, out)
	}
	after := whatTheNodeAnswers(t, home.Root)
	for key, was := range before.lookup {
		if now, answers := after.lookup[key]; !answers || fmt.Sprint(was) != fmt.Sprint(now) {
			t.Errorf("%s was %v and is %v after the file the command placed, so the file it writes is "+
				"not the file it prints", key, was, now)
		}
	}

	// Run again over the file it just wrote. An operator who repeats the command has a file they did not
	// decide to replace.
	second, err := runGenerateThroughRoot(t, home.Root, "--mode", "validator", "--write")
	if err == nil {
		t.Errorf("the second run replaced the file the first one wrote:\n%s", second)
	}
}
