package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"sort"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

type Validator struct {
	*fiber.Ctx

	contentType    utils.RequestContentType
	contentTypeRaw []byte

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

	if e = m.extractRequestKey(); e != nil {
		return
	}

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

func (m *Validator) encodeQueryArgs() error {
	rgs := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(rgs)

	rgs.ParseBytes(m.Body())
	if rgs.Len() == 0 {
		return errors.New("there is no args after query parsing")
	}

	// ?
	rgs.Sort(bytes.Compare)

	m.cacheKey.Put(rgs.QueryString())
	return nil
}

func (m *Validator) encodeFormData() (e error) {
	var form *multipart.Form
	if form, e = m.MultipartForm(); errors.Is(e, fasthttp.ErrNoMultipartForm) {
		return errors.New("BUG: multipart form without form")
	} else if e != nil {
		return
	}
	defer m.Request().RemoveMultipartFormFiles()

	var keys []string
	for k := range form.Value {
		keys = append(keys, k)
	}

	if len(keys) == 0 {
		return errors.New("form len is 0, seems it's empty")
	}

	sort.Strings(keys)

	bb := bytebufferpool.Get()
	defer bytebufferpool.Put(bb)

	for _, v := range keys {
		val := m.FormValue(v)
		if val == "" {
			// !!!
			// !!!
			// TODO
			// rlog.Warn().Msg("BUG: form value is empty, key - " + v)
			continue
		}

		bb.WriteString(fmt.Sprintf("%s=%s&", v, m.FormValue(v)))
	}

	m.cacheKey.Put(bb.B[:bb.Len()-1])
	return
}
