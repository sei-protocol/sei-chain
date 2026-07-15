package disktable

import (
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// compressionLoop compresses value bytes off the control-loop goroutine. It sits in front of the control
// loop: when compression is enabled, controlLoop.enqueue sends every control message to inputChannel,
// this loop compresses write requests, and forwards all messages (compressed writes and everything else,
// verbatim) to outputChannel (the control loop's controllerChannel) in arrival order.
//
// Forwarding all message types in order is what makes flush correct: a flush request travels the same
// channel behind the writes it must follow, so the control loop applies those writes first. Because this
// loop is single-threaded and finishes compressing a write before it reads the next message, any in-flight
// compression is complete before a following flush is forwarded; the ordering barrier is automatic.
type compressionLoop struct {
	// logger for the compression loop.
	logger *slog.Logger

	// errorMonitor is used to react to fatal errors anywhere in the disk table.
	errorMonitor *util.ErrorMonitor

	// algorithm is the compression algorithm applied to write-request values.
	algorithm types.CompressionAlgorithm

	// inputChannel receives messages from controlLoop.enqueue.
	inputChannel chan any

	// outputChannel forwards messages to the control loop (its controllerChannel).
	outputChannel chan any
}

// run processes messages until shutdown. It compresses write requests and forwards every message to the
// control loop in arrival order.
func (cl *compressionLoop) run() {
	for {
		select {
		case <-cl.errorMonitor.ImmediateShutdownRequired():
			return
		case message := <-cl.inputChannel:
			if req, ok := message.(*controlLoopWriteRequest); ok {
				if !cl.compress(req) {
					// compress panicked the DB via the error monitor; stop forwarding.
					return
				}
			}

			// Forward every message (compressed writes and all others) in arrival order.
			if err := util.Send(cl.errorMonitor, cl.outputChannel, message); err != nil {
				return
			}

			// The shutdown request is the last message the control loop will process; stop after
			// forwarding it so this goroutine does not outlive the table.
			if _, ok := message.(*controlLoopShutdownRequest); ok {
				return
			}
		}
	}
}

// compress fills req.compressedValues with the compressed form of each value. It returns false if
// compression failed (in which case it has already panicked the DB via the error monitor).
func (cl *compressionLoop) compress(req *controlLoopWriteRequest) bool {
	compressed := make([][]byte, len(req.values))
	for i, kv := range req.values {
		blob, err := types.Compress(cl.algorithm, kv.Value)
		if err != nil {
			cl.errorMonitor.Panic(fmt.Errorf("failed to compress value: %w", err))
			return false
		}
		compressed[i] = blob
	}
	req.compressedValues = compressed
	return true
}
