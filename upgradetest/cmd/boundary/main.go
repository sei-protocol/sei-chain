// Command boundary prints the upgrade boundary this build ships, so that a
// Makefile target or a CI step can select the boundary's test set without
// naming a version. Naming one there is how a workflow comes to run the test
// set for an upgrade that already shipped.
//
//	boundary         the boundary, as "v6.6 -> v6.7"
//	boundary from    the source version, as "v6.6"
//	boundary to      the target upgrade name, as "v6.7"
//	boundary tag     the build tag compiling its test, as "upgrade_v67"
//	boundary file    the app test file, as "upgrade_v67_test.go"
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sei-protocol/sei-chain/upgradetest"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "boundary: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	what := "boundary"
	switch len(args) {
	case 0:
	case 1:
		what = args[0]
	default:
		return fmt.Errorf("want at most one of boundary, from, to, tag or file, got %d arguments", len(args))
	}

	boundary, err := upgradetest.Current()
	if err != nil {
		return err
	}

	var answer string
	switch what {
	case "boundary":
		answer = boundary.String()
	case "from":
		answer = boundary.From
	case "to":
		answer = boundary.To
	case "tag":
		answer, err = boundary.Tag()
	case "file":
		answer, err = boundary.TestFile()
	default:
		return fmt.Errorf("unknown request %q; want boundary, from, to, tag or file", what)
	}
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, answer)
	return err
}
