package rewriter

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"sort"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type Rewriter struct {
	mu    sync.RWMutex
	ready bool
}

func NewRewriter() *Rewriter {
	return &Rewriter{}
}

// `skip` HandlerUnavailable response if IsInitialized returned true
func (m *Rewriter) IsInitialized(*fiber.Ctx) bool {
	return m.isAvailable()
}

func (*Rewriter) IsMultipartForm(c *fiber.Ctx) bool {
	_, e := c.MultipartForm()
	return !errors.Is(e, fasthttp.ErrNoMultipartForm)
}

func (m *Rewriter) isAvailable(val ...bool) bool {
	val = append(val, false)

	if len(val) == 1 {
		m.mu.RLock()
		defer m.mu.RUnlock()

		return m.ready
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.ready = val[1]
	return val[1]
}

func (*Rewriter) rewrite(c *fiber.Ctx) (e error) {

	var form *multipart.Form
	if form, e = c.Request().MultipartForm(); e != nil {
		return
	}
	defer c.Request().RemoveMultipartFormFiles()

	var formlen int
	if formlen = len(form.Value); formlen == 0 {
		return
	}

	var keys []string
	for k := range form.Value {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var buf bytes.Buffer // ? sync.Pool
	for _, v := range keys {
		buf.WriteString(fmt.Sprintf("%s=%s&", v, c.FormValue(v)))
	}

	c.Set("X-Parsed-Form", string(buf.Bytes())[0:buf.Len()-1])
	return
}
