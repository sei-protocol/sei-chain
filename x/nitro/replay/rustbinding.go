//go:build !codeanalysis
// +build !codeanalysis

package replay

// #include <stdlib.h>
// #include "replayer.h"
import "C"
import (
	"fmt"

	"github.com/sei-protocol/sei-chain/utils"
)

func callReplayer(accountFiles []string, programFiles []string, txFiles []string, outputDir string) (err error) {
	defer func() {
		if err := recover(); err != nil {
			err = fmt.Errorf("received error when calling replayer: %s", err)
		}
	}()
	C.replay(
		makeFilePaths(
			utils.Map(accountFiles, func(path string) C.ByteSliceView {
				return makeView([]byte(path))
			}),
		),
		makeFilePaths(
			utils.Map(programFiles, func(path string) C.ByteSliceView {
				return makeView([]byte(path))
			}),
		),
		makeFilePaths(
			utils.Map(txFiles, func(path string) C.ByteSliceView {
				return makeView([]byte(path))
			}),
		),
		makeView([]byte(outputDir)),
	)
	return nil
}
