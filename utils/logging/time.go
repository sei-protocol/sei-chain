package logging

import (
	"context"
	"time"

	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

var logger = seilog.NewLogger("utils", "logging")

func LogIfNotDoneAfter[R any](task func() (R, error), after time.Duration, label string) (R, error) {
	resultChan := make(chan R, 1)
	errChan := make(chan error, 1)
	panicChan := make(chan any, 1)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				panicChan <- err
			}
		}()
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
		case err := <-panicChan:
			// reraise panic in main goroutine
			panic(err)
		case <-time.After(after):
			loggingMetrics.logNotDoneAfter.Add(context.Background(), 1, otelmetric.WithAttributes(attribute.String("label", label)))
			logger.Error("operation still not finished", "label", label, "after", after)
		}
	}
}
