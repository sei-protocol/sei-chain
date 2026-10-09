package apphash

// AppHashIterator iterates over app hash data in ascending block height order. It is not safe for concurrent use.
type AppHashIterator interface {

	// Advances to the next app hash. Returns false once iteration is complete. After an error, only Close() may
	// be called.
	Next() (bool, error)

	// Returns the app hash at the current position. Valid only after Next() returns true.
	Entry() *AppHashData

	// Releases the iterator's resources.
	Close() error
}
