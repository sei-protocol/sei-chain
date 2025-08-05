package dbsync

import (
	"errors"
	"fmt"

	"github.com/gogo/protobuf/proto"
)

// Wrap implements the p2p Wrapper interface and wraps a state sync proto message.
func (m *Message) Wrap(pb proto.Message) error {
	switch msg := pb.(type) {
	case *MetadataRequest:
		m.Sum = &Message_MetadataRequest{MetadataRequest: msg}

	case *MetadataResponse:
		m.Sum = &Message_MetadataResponse{MetadataResponse: msg}

	case *FileRequest:
		m.Sum = &Message_FileRequest{FileRequest: msg}

	case *FileResponse:
		m.Sum = &Message_FileResponse{FileResponse: msg}

	case *LightBlockRequest:
		m.Sum = &Message_LightBlockRequest{LightBlockRequest: msg}

	case *LightBlockResponse:
		m.Sum = &Message_LightBlockResponse{LightBlockResponse: msg}

	case *ParamsRequest:
		m.Sum = &Message_ParamsRequest{ParamsRequest: msg}

	case *ParamsResponse:
		m.Sum = &Message_ParamsResponse{ParamsResponse: msg}

	default:
		return fmt.Errorf("unknown message: %T", msg)
	}

	return nil
}

// Unwrap implements the p2p Wrapper interface and unwraps a wrapped state sync
// proto message.
func (m *Message) Unwrap() (proto.Message, error) {
	switch msg := m.Sum.(type) {
	case *Message_MetadataRequest:
		return m.GetMetadataRequest(), nil

	case *Message_MetadataResponse:
		return m.GetMetadataResponse(), nil

	case *Message_FileRequest:
		return m.GetFileRequest(), nil

	case *Message_FileResponse:
		return m.GetFileResponse(), nil

	case *Message_LightBlockRequest:
		return m.GetLightBlockRequest(), nil

	case *Message_LightBlockResponse:
		return m.GetLightBlockResponse(), nil

	case *Message_ParamsRequest:
		return m.GetParamsRequest(), nil

	case *Message_ParamsResponse:
		return m.GetParamsResponse(), nil

	default:
		return nil, fmt.Errorf("unknown message: %T", msg)
	}
}

// Validate validates the message returning an error upon failure.
func (m *Message) Validate() error {
	if m == nil {
		return errors.New("message cannot be nil")
	}

	return nil
}
