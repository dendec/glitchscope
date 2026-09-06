package soloud

/*
#include <stdint.h>
*/
import "C"

import (
	"context"
	"runtime/cgo"
)

//export glitchscopeRenderCancelled
func glitchscopeRenderCancelled(handle C.uintptr_t) C.int {
	if cgo.Handle(handle).Value().(context.Context).Err() != nil {
		return 1
	}
	return 0
}
