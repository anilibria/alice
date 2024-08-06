package geoip

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

type GeoIPClient interface {
	Bootstrap()
	LookupCountryISO(ip string) (iso string, e error)
	IsReady() bool
}

type geoIPRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}

var geoipRecordPool = sync.Pool{
	New: func() interface{} {
		return &geoIPRecord{}
	},
}

func getGeoIPRecordPool() *geoIPRecord {
	r := geoipRecordPool.Get()
	return r.(*geoIPRecord)
}

func (m *geoIPRecord) reset() {
	*m = geoIPRecord{}
}

func lookupISOByIP(mu *sync.RWMutex, mxrd *maxminddb.Reader, rawip string) (iso string, e error) {
	mu.RLock()
	defer mu.RUnlock()

	var ip netip.Addr
	if ip, e = netip.ParseAddr(rawip); e != nil {
		e = errors.New(fmt.Sprintf("could not parse ip addr with netip library, ip - %+v - ", rawip) + e.Error())
		return
	}

	record := getGeoIPRecordPool()

	e = mxrd.Lookup(net.IP(ip.AsSlice()), &record)
	iso = record.Country.ISOCode

	record.reset()
	geoipRecordPool.Put(record)

	return
}
