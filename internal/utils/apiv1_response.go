package utils

import (
	"encoding/json"
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
	ApiResponseRaw struct {
		Status bool
		Data   *json.RawMessage
		Error  interface{}
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

var (
	apiResponseRawDataPool = sync.Pool{
		New: func() interface{} {
			return &ApiResponseRaw{}
		},
	}
	apiResponseWODataPool = sync.Pool{
		New: func() interface{} {
			return &ApiResponseWOData{
				Error: &ApiError{},
			}
		},
	}
	apiResponsePool = sync.Pool{
		New: func() interface{} {
			return &ApiResponse{
				Data:  &ApiResponseData{},
				Error: &ApiError{},
			}
		},
	}
)

func AcquireApiResponseRawData() *ApiResponseRaw {
	return apiResponseRawDataPool.Get().(*ApiResponseRaw)
}

func AcquireApiResponseWOData() *ApiResponseWOData {
	return apiResponseWODataPool.Get().(*ApiResponseWOData)
}

func AcquireApiResponse() *ApiResponse {
	return apiResponsePool.Get().(*ApiResponse)
}

func ReleaseApiResponseRawData(ar *ApiResponseRaw) {
	ar.Status, ar.Data = false, nil
	apiResponseRawDataPool.Put(ar)
}

func ReleaseApiResponseWOData(ar *ApiResponseWOData) {
	ar.Status = false
	ar.Error.Code, ar.Error.Message, ar.Error.Description = 0, "", ""
	apiResponseWODataPool.Put(ar)
}

func ReleaseApiResponse(ar *ApiResponse) {
	*ar = ApiResponse{
		Data:  &ApiResponseData{},
		Error: &ApiError{},
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

func RespondWithRawJSON(payload *json.RawMessage, w io.Writer) (e error) {
	apirsp := AcquireApiResponseRawData()
	defer ReleaseApiResponseRawData(apirsp)

	apirsp.Status, apirsp.Data = true, payload

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
