package service

import (
	"io"

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
