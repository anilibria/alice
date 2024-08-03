package utils

type ContextKey uint8

const (
	CKLogger ContextKey = iota
	CKCliCtx
	CKAbortFunc
	CKCache
	CKGeoIP
)
