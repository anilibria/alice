package utils

type RequestContentType uint8

const (
	CTInvalid RequestContentType = iota
	CTApplicationUrlencoded
	CTMultipartFormData
)

type FastUserValue uint8

const (
	UVCacheKey FastUserValue = iota
)
