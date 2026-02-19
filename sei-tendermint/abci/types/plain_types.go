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

// RequestLoadSnapshotChunk is the plain Go replacement for the legacy protobuf message.
type RequestLoadSnapshotChunk struct {
	Height uint64
	Format uint32
	Chunk  uint32
}

// ResponseLoadSnapshotChunk is the plain Go replacement for the legacy protobuf message.
type ResponseLoadSnapshotChunk struct {
	Chunk []byte
}

// RequestApplySnapshotChunk is the plain Go replacement for the legacy protobuf message.
type RequestApplySnapshotChunk struct {
	Index  uint32
	Chunk  []byte
	Sender string
}

// ResponseApplySnapshotChunk is the plain Go replacement for the legacy protobuf message.
type ResponseApplySnapshotChunk struct {
	Result        ResponseApplySnapshotChunk_Result
	RefetchChunks []uint32
	RejectSenders []string
}

// ResponseApplySnapshotChunk_Result mirrors the historical protobuf enum.
type ResponseApplySnapshotChunk_Result int32

const (
	ResponseApplySnapshotChunk_UNKNOWN         ResponseApplySnapshotChunk_Result = 0
	ResponseApplySnapshotChunk_ACCEPT          ResponseApplySnapshotChunk_Result = 1
	ResponseApplySnapshotChunk_ABORT           ResponseApplySnapshotChunk_Result = 2
	ResponseApplySnapshotChunk_RETRY           ResponseApplySnapshotChunk_Result = 3
	ResponseApplySnapshotChunk_RETRY_SNAPSHOT  ResponseApplySnapshotChunk_Result = 4
	ResponseApplySnapshotChunk_REJECT_SNAPSHOT ResponseApplySnapshotChunk_Result = 5
)
