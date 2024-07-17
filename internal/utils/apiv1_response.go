package utils

import (
	"io"

	"github.com/mailru/easyjson"
)

type (
	ApiResponse struct {
		Status bool
		Data   interface{} `json:"-"`
		Error  *ApiError
	}
	ApiError struct {
		Code        int
		Message     string
		Description string
	}
)

func newApiResponse(status int, msg, desc string) *ApiResponse {
	return &ApiResponse{
		Error: &ApiError{
			Code:        status,
			Message:     msg,
			Description: desc,
		},
	}
}

func RespondWithApiError(status int, msg, desc string, w io.Writer) (e error) {
	apirsp := newApiResponse(status, msg, desc)

	var buf []byte
	if buf, e = easyjson.Marshal(apirsp); e != nil {
		return
	}

	_, e = w.Write(buf)
	return
}

func UnmarshalApiResponse(payload []byte) (_ *ApiResponse, e error) {
	apirsp := new(ApiResponse)
	return apirsp, easyjson.Unmarshal(payload, apirsp)
}
