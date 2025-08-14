package scanner

type opErr struct {
	pos     int
	msg     string
	content string
}

func (o opErr) Error() string {
	return o.msg + "; ..." + o.content
}
