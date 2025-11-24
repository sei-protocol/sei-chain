package statesync

import (
	"errors"
	"fmt"
)

// Validate validates the message returning an error upon failure.
func (m *Message) Validate() error {
	if m == nil {
		return errors.New("message cannot be nil")
	}

	switch msg := m.Sum.(type) {
	case *Message_ChunkRequest:
		if m.GetChunkRequest().Height == 0 {
			return errors.New("height cannot be 0")
		}

	case *Message_ChunkResponse:
		if m.GetChunkResponse().Height == 0 {
			return errors.New("height cannot be 0")
		}
		if m.GetChunkResponse().Missing && len(m.GetChunkResponse().Chunk) > 0 {
			return errors.New("missing chunk cannot have contents")
		}
		if !m.GetChunkResponse().Missing && m.GetChunkResponse().Chunk == nil {
			return errors.New("chunk cannot be nil")
		}

	case *Message_SnapshotsRequest:

	case *Message_SnapshotsResponse:
		if m.GetSnapshotsResponse().Height == 0 {
			return errors.New("height cannot be 0")
		}
		if len(m.GetSnapshotsResponse().Hash) == 0 {
			return errors.New("snapshot has no hash")
		}
		if m.GetSnapshotsResponse().Chunks == 0 {
			return errors.New("snapshot has no chunks")
		}

	case *Message_LightBlockRequest:
		if m.GetLightBlockRequest().Height == 0 {
			return errors.New("height cannot be 0")
		}

	// light block validation handled by the backfill process
	case *Message_LightBlockResponse:

	case *Message_ParamsRequest:
		if m.GetParamsRequest().Height == 0 {
			return errors.New("height cannot be 0")
		}

	case *Message_ParamsResponse:
		resp := m.GetParamsResponse()
		if resp.Height == 0 {
			return errors.New("height cannot be 0")
		}

	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}

	return nil
}
