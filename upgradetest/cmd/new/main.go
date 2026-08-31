// Command new scaffolds the tagged app test file for a minor upgrade.
//
//	go run ./upgradetest/cmd/new -from v6.7 -to v6.8
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sei-protocol/sei-chain/upgradetest"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "new upgrade test: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("new", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var from, to, root string
	flags.StringVar(&from, "from", "", "source minor version, for example v6.7")
	flags.StringVar(&to, "to", "", "target minor version, for example v6.8")
	flags.StringVar(&root, "root", "app", "app directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if from == "" || to == "" {
		return fmt.Errorf("both -from and -to are required")
	}

	path, err := upgradetest.Scaffold(root, from, to)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "created %s\n", path); err != nil {
		return err
	}
	_, err = fmt.Fprintln(
		out,
		"next: replace the in-process and cross-version TODOs, then run make upgrade-test",
	)
	return err
}
