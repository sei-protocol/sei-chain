package utils

func PanicHandler(recoverCallback func(any)) func() {
	return func() {
		if err := recover(); err != nil {
			recoverCallback(err)
		}
	}
}
