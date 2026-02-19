package types

// RequestLoadLatest is the plain Go replacement for the legacy protobuf message.
type RequestLoadLatest struct{}

// ResponseLoadLatest is the plain Go replacement for the legacy protobuf message.
type ResponseLoadLatest struct{}

// RequestListSnapshots is the plain Go replacement for the legacy protobuf message.
type RequestListSnapshots struct{}

// ResponseListSnapshots is the plain Go replacement for the legacy protobuf message.
type ResponseListSnapshots struct {
	Snapshots []*Snapshot
}

// RequestOfferSnapshot is the plain Go replacement for the legacy protobuf message.
type RequestOfferSnapshot struct {
	Snapshot *Snapshot
	AppHash  []byte
}

// ResponseOfferSnapshot is the plain Go replacement for the legacy protobuf message.
type ResponseOfferSnapshot struct {
	Result ResponseOfferSnapshot_Result
}

// ResponseOfferSnapshot_Result mirrors the historical protobuf enum.
type ResponseOfferSnapshot_Result int32

const (
	ResponseOfferSnapshot_UNKNOWN       ResponseOfferSnapshot_Result = 0
	ResponseOfferSnapshot_ACCEPT        ResponseOfferSnapshot_Result = 1
	ResponseOfferSnapshot_ABORT         ResponseOfferSnapshot_Result = 2
	ResponseOfferSnapshot_REJECT        ResponseOfferSnapshot_Result = 3
	ResponseOfferSnapshot_REJECT_FORMAT ResponseOfferSnapshot_Result = 4
	ResponseOfferSnapshot_REJECT_SENDER ResponseOfferSnapshot_Result = 5
)
