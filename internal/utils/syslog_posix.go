//go:build !windows && !plan9

package utils

import (
	"bytes"
	"fmt"
	"io"
	"log/syslog"
	"net"
	"time"

	gosyslog "github.com/leodido/go-syslog/v4/rfc5424"
	"github.com/urfave/cli/v2"
)

func SetUpSyslogWriter(c *cli.Context) (_ io.Writer, e error) {
	// return syslog.Dial(c.String("syslog-proto"), c.String("syslog-server"), syslog.LOG_INFO, c.String("syslog-tag"))
	// return NewWrapped(syslog.Dial(c.String("syslog-proto"), c.String("syslog-server"), syslog.LOG_INFO, c.String("syslog-tag")))
	return NewSyslog5424().Dial(c.String("syslog-proto"), c.String("syslog-server"))
}

// the main idea of this wrapper was stolen from
// https://github.com/deis/deis/pull/4876/files

const (
	MAX_UDP_MSG_BYTES = 65400
	// MAX_TCP_MSG_BYTES = 1048576
)

type SyslogWrapped struct {
	*syslog.Writer
}

func NewWrapped(w *syslog.Writer, e error) (io.Writer, error) {
	if e != nil {
		return nil, e
	}

	return &SyslogWrapped{
		Writer: w,
	}, nil
}

func (m *SyslogWrapped) Write(b []byte) (_ int, e error) {
	// Truncate the message if it's too long to fit in a single UDP packet.
	// Get the bytes first.  If the string has non-UTF8 chars, the number of
	// bytes might exceed the number of characters and it would be good to
	// know that up front.

	// dataBytes := []byte(b)
	// var dataBytes []byte

	if len(b) > MAX_UDP_MSG_BYTES {
		// bb := bytebufferpool.Get()
		// bb.Write(append(b[:MAX_UDP_MSG_BYTES-3], "..."...))
		// fmt.Printf("cap %d len %d \n", cap(bb.B), bb.Len())
		// return m.Writer.Write(bb.B)

		// Truncate the bytes and add ellipses.
		// dataBytes = append(b[:MAX_UDP_MSG_BYTES-3], "..."...)
		// dataBytes = make([]byte, MAX_UDP_MSG_BYTES, MAX_UDP_MSG_BYTES)
		// copy(dataBytes, b[:MAX_UDP_MSG_BYTES-3])
		// dataBytes = append(dataBytes, "..."...)

		dataBytes := bytes.NewBuffer(nil)
		dataBytes.Write(b[:MAX_UDP_MSG_BYTES-3])
		dataBytes.WriteByte(byte('.'))
		dataBytes.WriteByte(byte('.'))
		dataBytes.WriteByte(byte('.'))

		fmt.Printf("utils/syslog_unix - the message has been truncated from %d to %d\n", len(b), dataBytes.Len())
		fmt.Printf("utils/syslog_unix - cap %d len %d\n", dataBytes.Cap(), dataBytes.Len())

		var n int
		var err error
		if n, err = m.Writer.Write(dataBytes.Bytes()); err != nil {
			fmt.Printf("there is error from Write - %s\n", err.Error())
			return n, err
		}

		fmt.Printf("written %d bytes\n", n)

		return dataBytes.Len(), nil
	}

	return m.Writer.Write(b)
}

func (m *SyslogWrapped) Close() error {
	return m.Writer.Close()
}

type Syslog5424 struct {
	conn net.Conn
}

func NewSyslog5424() *Syslog5424 {
	return &Syslog5424{}
}

func (m *Syslog5424) Dial(network, raddr string) (io.Writer, error) {
	return m, m.connect(network, raddr)
}

func (m *Syslog5424) Write(b []byte) (_ int, e error) {
	return m.write(b)
}

//

func (m *Syslog5424) connect(network, raddr string) (e error) {
	if m.conn, e = net.Dial(network, raddr); e != nil {
		return
	}

	return
}

func (m *Syslog5424) write(b []byte) (_ int, e error) {

	msg := &gosyslog.SyslogMessage{}
	msg.SetMessage(string(b))
	msg.SetTimestamp(time.Now().Format(gosyslog.RFC3339MICRO))
	msg.SetPriority(191)
	msg.SetVersion(1)

	res, e := msg.String()
	if e != nil {
		return
	}

	fmt.Fprint(m.conn, res)
	return len(b), e
}
