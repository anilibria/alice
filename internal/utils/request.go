package utils

import "github.com/rs/zerolog"

const HTTPAccessLogLevel = zerolog.InfoLevel

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
