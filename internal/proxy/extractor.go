package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"sort"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"

	futils "github.com/gofiber/fiber/v2/utils"
)

type Extractor struct {
	*fiber.Ctx

	contentType []byte

	log *zerolog.Logger
}

func NewExtractor(c *fiber.Ctx) *Extractor {
	return &Extractor{
		contentType: c.Request().Header.ContentType(),

		Ctx: c,
		// log: c.Value(utils.CKLogger).(*zerolog.Logger),
	}
}

func (m *Extractor) RequestCacheKey() (key []byte, e error) {
	var rtype utils.RequestContentType
	if rtype = m.RequestType(); rtype == utils.CTInvalid {
		ctype := futils.UnsafeString(m.contentType)
		return nil, errors.New("invalid request content-type: " + ctype)
	}

	switch rtype {
	case utils.CTApplicationUrlencoded:
		key, e = m.EncodeQueryArgs()
	case utils.CTMultipartFormData:
		key, e = m.EncodeFormData()
	}

	return
}

// Maybe use fiber's utils? - https://docs.gofiber.io/api/ctx#is
// TODO
func (m *Extractor) RequestType() utils.RequestContentType {
	ctype := futils.UnsafeString(m.contentType)

	if idx := bytes.IndexByte(m.contentType, byte(';')); idx > 0 {
		ctype = futils.UnsafeString(m.contentType[:idx])
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

func (m *Extractor) EncodeQueryArgs() (key []byte, e error) {
	rgs := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(rgs)

	rgs.ParseBytes(m.Body())
	if rgs.Len() == 0 {
		return nil, errors.New("there is no args after query parsing")
	}

	rgs.Sort(bytes.Compare)

	return rgs.QueryString(), e
}

func (m *Extractor) EncodeFormData() (_ []byte, e error) {
	var form *multipart.Form
	if form, e = m.MultipartForm(); errors.Is(e, fasthttp.ErrNoMultipartForm) {
		m.log.Warn().Msg("BUG: multipart form without form")
		return
	} else if e != nil {
		return
	}
	defer m.Request().RemoveMultipartFormFiles()

	var formlen int
	if formlen = len(form.Value); formlen == 0 {
		return nil, errors.New("form len is 0, seems it's empty")
	}

	var keys []string
	for k := range form.Value {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var buf bytes.Buffer // ? sync.Pool
	for _, v := range keys {
		val := m.FormValue(v)
		if val == "" {
			m.log.Warn().Msg("BUG: form value is empty, key - " + v)
			continue
		}

		buf.WriteString(fmt.Sprintf("%s=%s&", v, m.FormValue(v)))
	}

	return buf.Bytes()[:buf.Len()-1], e
}
