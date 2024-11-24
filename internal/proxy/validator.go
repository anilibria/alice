package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"sync"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

type Validator struct {
	*fiber.Ctx

	contentType    utils.RequestContentType
	contentTypeRaw []byte

	requestArgs *fasthttp.Args

	cacheKey *Key

	customs CustomHeaders
}

var validatorPool = sync.Pool{
	New: func() interface{} {
		return new(Validator)
	},
}

func AcquireValidator(c *fiber.Ctx, ctr []byte) (v *Validator) {
	v = validatorPool.Get().(*Validator)

	v.Ctx, v.contentTypeRaw = c, ctr
	v.cacheKey = AcquireKey()
	return
}

func ReleaseValidator(v *Validator) {
	if v.requestArgs != nil {
		fasthttp.ReleaseArgs(v.requestArgs)
	}

	v.Reset()
	validatorPool.Put(v)
}

//
//
//

func (m *Validator) ValidateRequest() (e error) {
	if m.contentType = m.validateContentType(); m.contentType == utils.CTInvalid {
		return fmt.Errorf("invalid request content-type - %s",
			futils.UnsafeString(m.contentTypeRaw))
	}

	m.validateCustomHeaders()

	m.requestArgs = fasthttp.AcquireArgs()

	if e = m.extractRequestKey(); e != nil {
		return
	}

	if !m.isArgsWhitelisted() {
		return errors.New("invalid api arguments detected")
	}

	if !m.isQueryWhitelisted() {
		return errors.New("invalid query detected")
	}

	if m.isQueryBypassListed() {
		m.customs = m.customs | CHCacheBypass
	}

	// delete or update cache key for further request processing
	// controlled by CustomHeaders
	m.postValidationMutate(m.requestArgs.QueryString())

	m.Context().SetUserValue(utils.UVCacheKey, m.cacheKey)
	return
}

func (m *Validator) IsQueryEqual(equal []byte) bool {
	return m.queryLookup(equal)
}

func (m *Validator) Arg(lookup []byte) ([]byte, bool) {
	return m.argLookup(lookup)
}

func (m *Validator) ArgsLen() int {
	return m.requestArgs.Len()
}

func (m *Validator) Reset() {
	m.Context().RemoveUserValue(utils.UVCacheKey)
	ReleaseKey(m.cacheKey)

	m.contentType = 0
	m.contentTypeRaw = m.contentTypeRaw[:0]

	m.customs = 0
	m.requestArgs, m.Ctx = nil, nil
}

//
//
//

func (m *Validator) validateContentType() utils.RequestContentType {
	ctype := futils.UnsafeString(m.contentTypeRaw)

	if idx := bytes.IndexByte(m.contentTypeRaw, byte(';')); idx > 0 {
		ctype = futils.UnsafeString(m.contentTypeRaw[:idx])
	}

	switch ctype {
	case "application/x-www-form-urlencoded":
		return utils.CTApplicationUrlencoded
	case "multipart/form-data":
		return utils.CTMultipartFormData
	default:
		return utils.CTInvalid
	}

}

func (m *Validator) validateCustomHeaders() {
	for header, ch := range Stoch {
		val := m.Request().Header.PeekBytes(futils.UnsafeBytes(header))
		if len(val) != 0 {
			m.customs = m.customs | ch

			if zerolog.GlobalLevel() <= zerolog.DebugLevel {
				rlog(m.Ctx).Trace().Msg("found custom header " + header)
			}
		}
	}

	// some another header validation...
}

func (m *Validator) postValidationMutate(cachekey []byte) {
	has := func(chflag CustomHeaders) bool {
		return m.customs&chflag != 0
	}

	// key is empty, so if we need bypass the cache just return
	if has(CHCacheBypass) {
		return
	}

	// override request cache-key if requested
	if has(CHCacheKeyOverride) {
		key := m.Request().Header.Peek(CHtos[CHCacheKeyOverride])
		m.cacheKey.Put(key)
		return
	}

	// mutate request cache-key
	if has(CHCacheKeyPrefix) || has(CHCacheKeySuffix) {
		bb := bytebufferpool.Get()
		defer bytebufferpool.Put(bb)

		bb.Write(m.Request().Header.Peek(CHtos[CHCacheKeyPrefix]))
		bb.Write(cachekey)
		bb.Write(m.Request().Header.Peek(CHtos[CHCacheKeySuffix]))

		m.cacheKey.Put(bb.Bytes())
		return
	}

	// put key without mutations
	m.cacheKey.Put(cachekey)
}

func (m *Validator) extractRequestKey() (e error) {
	// get requests content-type
	switch m.contentType {
	case utils.CTApplicationUrlencoded:
		e = m.encodeQueryArgs()
	case utils.CTMultipartFormData:
		e = m.encodeFormData()
	}

	return
}

func (m *Validator) encodeQueryArgs() (_ error) {
	if len(m.Body()) == 0 {
		return errors.New("empty body received")
	}
	m.requestArgs.ParseBytes(m.Body())

	if m.requestArgs.Len() == 0 {
		return errors.New("there is no args after query parsing")
	}

	// ?
	m.requestArgs.Sort(bytes.Compare)
	return
}

func (m *Validator) encodeFormData() (e error) {
	var form *multipart.Form
	if form, e = m.MultipartForm(); errors.Is(e, fasthttp.ErrNoMultipartForm) {
		return errors.New("BUG: multipart form without form")
	} else if e != nil {
		return
	}
	defer m.Request().RemoveMultipartFormFiles()

	if len(form.Value) == 0 {
		return errors.New("there is no form-data args after form parsing")
	}

	for k, v := range form.Value {
		m.requestArgs.Add(k, v[0])

		if zerolog.GlobalLevel() <= zerolog.DebugLevel {
			rlog(m.Ctx).Trace().Msg("parsed form value " + k + " - " + v[0])
		}
	}

	// TODO - with go1.21.0 we can use:
	//
	// m.requestArgs.Sort(func(x, y []byte) int {
	// 	return cmp.Compare(strings.ToLower(a), strings.ToLower(b))
	// })
	//
	// ? but in 1.19
	m.requestArgs.Sort(bytes.Compare)

	// more info - https://pkg.go.dev/slices#SortFunc
	return
}

var declinedKeysPool = sync.Pool{
	New: func() interface{} {
		dk := make([]string, 0)
		return &dk
	},
}

func (m *Validator) isArgsWhitelisted() (_ bool) {
	// []string pool without allocations
	// researched here - https://vk.cc/cys872
	declinedKeysPtr := declinedKeysPool.Get().(*[]string)
	declinedKeys := *declinedKeysPtr

	m.requestArgs.VisitAll(func(key, _ []byte) {
		if _, ok := postArgsWhitelist[futils.UnsafeString(key)]; !ok {
			declinedKeys = append(declinedKeys, futils.UnsafeString(key))
		}
	})

	var ok bool = true
	if len(declinedKeys) != 0 {
		if zerolog.GlobalLevel() < zerolog.InfoLevel {
			for _, key := range declinedKeys {
				rlog(m.Ctx).Debug().Msg("Invalid args-key detected - " + key)
			}
		}

		ok = false
	}

	declinedKeys = declinedKeys[:0]
	*declinedKeysPtr = declinedKeys // copy the stack header over to the heap
	declinedKeysPool.Put(declinedKeysPtr)

	return ok
}

func (m *Validator) isQueryWhitelisted() (ok bool) {
	var query []byte
	if query = m.requestArgs.PeekBytes([]byte("query")); len(query) == 0 {
		return true
	}

	if _, ok = queryWhitelist[futils.UnsafeString(query)]; !ok {
		rlog(m.Ctx).Debug().Msg("Invalid query-key detected - " + futils.UnsafeString(query))
	}

	return
}

func (m *Validator) isQueryBypassListed() (ok bool) {
	var query []byte
	if query = m.requestArgs.PeekBytes([]byte("query")); len(query) == 0 {
		return true
	}

	_, ok = queryBypasslist[futils.UnsafeString(query)]
	return ok
}

func (m *Validator) queryLookup(equal []byte) (_ bool) {
	var query []byte
	if query = m.requestArgs.PeekBytes([]byte("query")); len(query) == 0 {
		return
	}

	return bytes.Equal(query, equal)
}

func (m *Validator) argLookup(lookup []byte) ([]byte, bool) {
	query := m.requestArgs.PeekBytes(lookup)

	return query, len(query) != 0
}
