package evmonlyapp

// CursorRowForTesting returns the module, key and value of the cursor row that the application
// commits with block height. Intended for use in tests only.
func CursorRowForTesting(height int64) (string, []byte, []byte) {
	return evmOnlyCursorModule, []byte(evmOnlyCursorKey), evmOnlyCursor{height: height}.encode()
}
