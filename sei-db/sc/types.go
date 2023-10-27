package sc

type StateCommit interface {
	Commit() (int64, error)
}
