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
	appname := fmt.Sprintf("%s/%s", c.App.Name, c.App.Version)

	return &ProxyClient{
		HostClient: &fasthttp.HostClient{
			Addr: c.String("proxy-dst-server"),

			Name: "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:124.0) Gecko/20100101 Firefox/124.0 " + appname,

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
