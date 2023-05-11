package utils

const ErrorByteLimit = 50

func GetTruncatedErrors(err error) string {
	return err.Error()[:ErrorByteLimit]
}
