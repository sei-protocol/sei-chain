package utils

const ErrorByteLimit = 50

func GetTruncatedErrors(err error) string {
	errStr := err.Error()
	if len(errStr) <= ErrorByteLimit {
		return errStr
	}
	return errStr[:ErrorByteLimit]
}
