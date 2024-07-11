package utils

type RequestContentType uint8

const (
	CTInvalid RequestContentType = iota
	CTApplicationUrlencoded
	CTMultipartFormData
)
