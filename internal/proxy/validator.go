package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"
)

type Validator struct {
	*fiber.Ctx

	contentType    utils.RequestContentType
	contentTypeRaw []byte

	requestArgs *fasthttp.Args

	cacheKey *Key
}

func (*Proxy) NewValidator(c *fiber.Ctx) *Validator {
	return &Validator{
		contentTypeRaw: c.Request().Header.ContentType(),

		cacheKey: AcquireKey(),

		// TODO -- logger
		// TODO -- see line 134
		// log: c.Value(utils.CKLogger).(*zerolog.Logger),

		Ctx: c,
	}
}

func (m *Validator) ValidateRequest() (e error) {
	if m.contentType = m.validateContentType(); m.contentType == utils.CTInvalid {
		return fmt.Errorf("invalid request content-type - %s",
			futils.UnsafeString(m.contentTypeRaw))
	}

	m.requestArgs = fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(m.requestArgs)

	if e = m.extractRequestKey(); e != nil {
		return
	}

	if !m.isArgsWhitelisted() {
		return errors.New("invalid api arguments detected")
	}

	if !m.isQueryWhitelisted() {
		return errors.New("invalid query detected")
	}

	m.cacheKey.Put(m.requestArgs.QueryString())
	m.Context().SetUserValue(utils.UVCacheKey, m.cacheKey)
	return
}

func (m *Validator) Destroy() {
	ReleaseKey(m.cacheKey)
	m.Context().RemoveUserValue(utils.UVCacheKey)
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

func (m *Validator) extractRequestKey() (e error) {
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

func (m *Validator) isArgsWhitelisted() (_ bool) {
	// TODO too much allocations here:
	declinedKeys := make(chan []byte, m.requestArgs.Len())

	m.requestArgs.VisitAll(func(key, value []byte) {
		if _, ok := postArgsWhitelist[futils.UnsafeString(key)]; !ok {
			declinedKeys <- key
		}
	})
	close(declinedKeys)

	if len(declinedKeys) != 0 {
		if zerolog.GlobalLevel() < zerolog.InfoLevel {
			for key := range declinedKeys {
				fmt.Println("Invalid key detected - " + futils.UnsafeString(key))
			}
		}

		return
	}

	return true
}

func (m *Validator) isQueryWhitelisted() (ok bool) {
	var query []byte
	if query = m.requestArgs.PeekBytes([]byte("query")); len(query) == 0 {
		return true
	}

	_, ok = queryWhitelist[futils.UnsafeString(query)]
	return
}
