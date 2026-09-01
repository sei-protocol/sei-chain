package bootstrap

// CrashRecover is not implemented. It returns nil without reconciling store heights.
//
// Once implemented it recovers opened stores on startup so that:
//  1. Every store is at or below the block store's height.
//  2. Every store other than the block store is on the same height.
func (m *GigaStorageManager) CrashRecover() error {
	return nil
}
