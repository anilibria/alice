package proxy

import (
	"fmt"

	"github.com/urfave/cli/v2"
	"github.com/valyala/fasthttp"
)

type ProxyClient struct {
	*fasthttp.HostClient
}

func NewClient(c *cli.Context) *ProxyClient {
	return &ProxyClient{
		HostClient: &fasthttp.HostClient{
			// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/User-Agent#crawler_and_bot_ua_strings
			Name: fmt.Sprintf("Mozilla/5.0 (compatible; %s/%s; +https://anilibria.top/support)",
				c.App.Name, c.App.Version),

			Addr: c.String("proxy-dst-server"),

			MaxConns: c.Int("proxy-max-conns-per-host"),

			ReadTimeout:         c.Duration("proxy-read-timeout"),
			WriteTimeout:        c.Duration("proxy-write-timeout"),
			MaxIdleConnDuration: c.Duration("proxy-idle-timeout"),
			MaxConnDuration:     c.Duration("proxy-conn-timeout"),

			DisableHeaderNamesNormalizing: true,
			DisablePathNormalizing:        true,
			NoDefaultUserAgentHeader:      true,

			Dial: (&fasthttp.TCPDialer{
				Concurrency:      c.Int("proxy-tcpdial-concurr"),
				DNSCacheDuration: c.Duration("proxy-dns-cache-dur"),
			}).Dial,

			// !!!
			// !!!
			// !!!
			// ? DialTimeout
		},
	}
}
