package mock

// MockEmptyAcknowledgement implements the exported.Acknowledgement interface and always returns an empty byte string as Response
type MockEmptyAcknowledgement struct {
	Response []byte
}

// NewMockEmptyAcknowledgement returns a new instance of MockEmptyAcknowledgement
func NewMockEmptyAcknowledgement() MockEmptyAcknowledgement {
	return MockEmptyAcknowledgement{
		Response: []byte{},
	}
}

// Success implements the Acknowledgement interface
func (ack MockEmptyAcknowledgement) Success() bool {
	return true
}

// Acknowledgement implements the Acknowledgement interface
func (ack MockEmptyAcknowledgement) Acknowledgement() []byte {
	return []byte{}
}
