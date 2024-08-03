package geoip

type GeoIPClient interface {
	Bootstrap()
	// IsReady() bool
}
