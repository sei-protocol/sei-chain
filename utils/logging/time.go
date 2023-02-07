package logging

import (
	"fmt"
	"time"

	"github.com/tendermint/tendermint/libs/log"
)

func LogIfNotDoneAfter[R any](logger log.Logger, task func() (R, error), after time.Duration, label string) (R, error) {
	resultChan := make(chan R, 1)
	errChan := make(chan error, 1)
	go func() {
		res, err := task()
		if err != nil {
			errChan <- err
		} else {
			resultChan <- res
		}
	}()
	for {
		select {
		case res := <-resultChan:
			return res, nil
		case err := <-errChan:
			var res R
			return res, err
		case <-time.After(after):
			logger.Error(fmt.Sprintf("%s still not finished after %s", label, after))
		}
	}
}
