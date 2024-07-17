package service

import (
	"io"
	"sync"

	"github.com/gofiber/fiber/v2"
	easyjson "github.com/mailru/easyjson"
)

type (
	FiberErrorResponse struct {
		Status bool
		Data   interface{}
		Error  *ResponseError
	}
	ResponseError struct {
		Code        int
		Message     string
		Description string
	}
)

func newFiberResponseError(status int, msg, desc string) *FiberErrorResponse {
	return &FiberErrorResponse{
		Error: &ResponseError{
			Code:        status,
			Message:     msg,
			Description: desc,
		},
	}
}

func respondWithError(status int, msg, desc string, w io.Writer) (e error) {
	err := newFiberResponseError(status, msg, desc)

	var buf []byte
	if buf, e = easyjson.Marshal(err); e != nil {
		return
	}

	_, e = w.Write(buf)
	return
}

//
//
//

// TODO 2delete
// I think this block of code is not profitable
// so may be it must be reverted

var ferrPool = sync.Pool{
	New: func() interface{} {
		return new(fiber.Error)
	},
}

func AcquireFErr() *fiber.Error {
	return ferrPool.Get().(*fiber.Error)
}

func ReleaseFErr(e *fiber.Error) {
	// ? is it required
	e.Code, e.Message = 0, ""
	ferrPool.Put(e)
}
