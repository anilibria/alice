package utils

import (
	"io"
	"sync"

	"github.com/mailru/easyjson"
)

type (
	ApiResponse struct {
		Status bool
		Data   *ApiResponseData
		Error  *ApiError
	}
	ApiResponseWOData struct {
		Status bool
		Data   interface{} `json:"-"`
		Error  *ApiError
	}
	ApiResponseData struct {
		Code string
	}
	ApiError struct {
		Code        int
		Message     string
		Description string
	}
)

var apiResponseWODataPool = sync.Pool{
	New: func() interface{} {
		return &ApiResponseWOData{
			Error: &ApiError{},
		}
	},
}

func AcquireApiResponseWOData() *ApiResponseWOData {
	return apiResponseWODataPool.Get().(*ApiResponseWOData)
}

func ReleaseApiResponseWOData(ar *ApiResponseWOData) {
	ar.Status = false
	ar.Error.Code, ar.Error.Message, ar.Error.Description = 0, "", ""
	apiResponseWODataPool.Put(ar)
}

var apiResponsePool = sync.Pool{
	New: func() interface{} {
		return &ApiResponse{
			Data:  &ApiResponseData{},
			Error: &ApiError{},
		}
	},
}

func AcquireApiResponse() *ApiResponse {
	return apiResponsePool.Get().(*ApiResponse)
}

func ReleaseApiResponse(ar *ApiResponse) {
	ar.Status = false
	if ar.Error != nil {
		ar.Error.Code, ar.Error.Message, ar.Error.Description = 0, "", ""
	}
	if ar.Data != nil {
		ar.Data.Code = ""
	}
	apiResponsePool.Put(ar)
}

func RespondWithApiError(status int, msg, desc string, w io.Writer) (e error) {
	apirsp := AcquireApiResponse()
	defer ReleaseApiResponse(apirsp)

	apirsp.Error.Code, apirsp.Error.Message, apirsp.Error.Description =
		status, msg, desc
	apirsp.Data = nil

	var buf []byte
	if buf, e = easyjson.Marshal(apirsp); e != nil {
		return
	}

	_, e = w.Write(buf)
	return
}

func RespondWithRandomRelease(code string, w io.Writer) (e error) {
	apirsp := AcquireApiResponse()
	defer ReleaseApiResponse(apirsp)

	apirsp.Status, apirsp.Data.Code, apirsp.Error = true, code, nil

	var buf []byte
	if buf, e = easyjson.Marshal(apirsp); e != nil {
		return
	}

	_, e = w.Write(buf)
	return
}

func UnmarshalApiResponse(payload []byte) (_ *ApiResponseWOData, e error) {
	apirsp := AcquireApiResponseWOData()
	return apirsp, easyjson.Unmarshal(payload, apirsp)
}
