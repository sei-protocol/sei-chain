//go:build goexperiment.simd && amd64

#include "textflag.h"

// func vzeroupper()
TEXT ·vzeroupper(SB), NOSPLIT, $0-0
	VZEROUPPER
	RET
