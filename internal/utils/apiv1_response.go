package utils

import (
	"io"
	"sync"

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

var apiResponsePool = sync.Pool{
	New: func() interface{} {
		return &ApiResponse{
			Error: &ApiError{},
		}
	},
}

func AcquireApiResponse() *ApiResponse {
	return apiResponsePool.Get().(*ApiResponse)
}

func ReleaseApiResponse(ar *ApiResponse) {
	ar.Status = false
	ar.Error.Code, ar.Error.Message, ar.Error.Description = 0, "", ""
	apiResponsePool.Put(ar)
}

func RespondWithApiError(status int, msg, desc string, w io.Writer) (e error) {
	apirsp := AcquireApiResponse()
	apirsp.Error.Code, apirsp.Error.Message, apirsp.Error.Description =
		status, msg, desc
	defer ReleaseApiResponse(apirsp)

	var buf []byte
	if buf, e = easyjson.Marshal(apirsp); e != nil {
		return
	}

	_, e = w.Write(buf)
	return
}

func UnmarshalApiResponse(payload []byte) (_ *ApiResponse, e error) {
	apirsp := AcquireApiResponse()
	return apirsp, easyjson.Unmarshal(payload, apirsp)
}
