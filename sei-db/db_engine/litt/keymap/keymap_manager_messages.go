package keymap

import "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"

// This file contains various messages that can be sent to the keymap manager via KeymapManager.enqueue.

// keymapManagerMessage is an interface for messages sent to the keymap manager loop.
type keymapManagerMessage interface {
	unimplemented()
}

// keymapManagerWriteRequest is a request to write keys to the keymap.
type keymapManagerWriteRequest struct {
	keymapManagerMessage

	// keys is the batch of keys to write.
	keys []types.ScopedKey
}

// keymapManagerDeleteRequest is a request to delete keys from the keymap.
type keymapManagerDeleteRequest struct {
	keymapManagerMessage

	// keys is the batch of keys to delete.
	keys []types.ScopedKey
}

// keymapManagerFlushRequest is a request to flush the keymap manager. All prior write requests are guaranteed
// to have been processed by the time the response is sent.
type keymapManagerFlushRequest struct {
	keymapManagerMessage

	// responseChan produces a value when the flush is complete.
	responseChan chan struct{}
}

// keymapManagerShutdownRequest is a request to shut down the keymap manager loop. All prior write requests
// are guaranteed to have been processed by the time the response is sent.
type keymapManagerShutdownRequest struct {
	keymapManagerMessage

	// responseChan produces a value when the shutdown is complete.
	responseChan chan struct{}
}
