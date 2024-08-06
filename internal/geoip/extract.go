package geoip

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"strings"

	"github.com/rs/zerolog"
)

func extractTarGzArchive(log *zerolog.Logger, dst io.Writer, src io.Reader) (e error) {
	var rd *gzip.Reader
	if rd, e = gzip.NewReader(src); e != nil {
		return
	}

	return extractTarArchive(log, dst, rd)
}

func extractTarArchive(log *zerolog.Logger, dst io.Writer, src io.Reader) (e error) {
	tr := tar.NewReader(src)
	for {
		var hdr *tar.Header
		hdr, e = tr.Next()

		if e == io.EOF {
			break // End of archive
		} else if e != nil {
			return
		}

		m.log.Trace().Msg("found file in maxmind tar archive - " + hdr.Name)
		if !strings.HasSuffix(hdr.Name, "mmdb") {
			continue
		}

		m.log.Trace().Msg("found mmdb file, copy to temporary file")

		var written int64
		if written, e = io.Copy(dst, tr); e != nil { // skipcq: GO-S2110 decompression bomb isn't possible here
			return
		}

		m.log.Debug().Msgf("parsed response has written in temporary file with %d bytes", written)
		break
	}

	return
}
