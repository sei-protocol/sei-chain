package replay

/*
# include "replayer.h"
*/
import "C"

import "unsafe"

type (
	cusize = C.uintptr_t
	cu8    = C.uint8_t
)

type (
	cu8_ptr   = *C.uint8_t
	cview_ptr = *C.ByteSliceView
)

// makeView creates a view into the given byte slice what allows Rust code to read it.
// The byte slice is managed by Go and will be garbage collected. Use runtime.KeepAlive
// to ensure the byte slice lives long enough.
func makeView(s []byte) C.ByteSliceView {
	if s == nil {
		return C.ByteSliceView{ptr: cu8_ptr(nil), len: cusize(0)}
	}

	// In Go, accessing the 0-th element of an empty array triggers a panic. That is why in the case
	// of an empty `[]byte` we can't get the internal heap pointer to the underlying array as we do
	// below with `&data[0]`. https://play.golang.org/p/xvDY3g9OqUk
	if len(s) == 0 {
		return C.ByteSliceView{ptr: cu8_ptr(nil), len: cusize(0)}
	}

	return C.ByteSliceView{
		ptr: cu8_ptr(unsafe.Pointer(&s[0])),
		len: cusize(len(s)),
	}
}

func makeFilePaths(views []C.ByteSliceView) C.FilePaths {
	if views == nil {
		return C.FilePaths{paths: cview_ptr(nil), count: cusize(0)}
	}

	if len(views) == 0 {
		return C.FilePaths{paths: cview_ptr(nil), count: cusize(0)}
	}

	return C.FilePaths{
		paths: cview_ptr(unsafe.Pointer(&views[0])),
		count: cusize(len(views)),
	}
}
