//
// Copyright 2020 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package main

import (
	"fmt"
	"runtime"

	"github.com/labstack/echo"
	"github.com/pkg/errors"
)

type stack []uintptr

func (s *stack) StackTrace() errors.StackTrace {
	f := make([]errors.Frame, len(*s))
	for i := 0; i < len(f); i++ {
		f[i] = errors.Frame((*s)[i])
	}
	return f
}

// stackError is a dummy error type that simply stores a stack of frames.
type stackError struct {
	stack errors.StackTrace
}

// StackTrace is necessary to implement the stackTracer interface in pkg/errors.
func (e *stackError) StackTrace() errors.StackTrace {
	return e.stack
}

func (e *stackError) Error() string {
	if len(e.stack) == 0 {
		return "stackError with no stack!"
	}

	frame := e.stack[0]
	pc := uintptr(frame) - 1
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return fmt.Sprintf("caller site %#x", pc)
	}
	file, line := fn.FileLine(pc)
	return fmt.Sprintf("caller site %s:%d(%s)", file, line, fn.Name())
}

// newHTTPError creates an echo.HTTPError object and sets its "Internal" error
// to a dummy error object that simply stores the stack at the site of the
// caller, for the logging stack to extract later.
func newHTTPError(code int, message ...interface{}) *echo.HTTPError {
	err := echo.NewHTTPError(code, message...)
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:])
	var st stack = pcs[0:n]
	err.SetInternal(&stackError{st.StackTrace()})
	return err
}

