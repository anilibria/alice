package anilibria

import (
	"github.com/goccy/go-json"
)

type (
	Release struct {
		Id          uint
		Code        string
		BlockedInfo *ReleaseBlockedInfo `json:"blockedInfo"`

		raw RawRelease
	}
	ReleaseBlockedInfo struct {
		Blocked               bool
		Reason                string
		IsBlockedInGeo        []string `json:"is_blocked_in_geo"`
		IsBlockedByCopyrights bool     `json:"is_blocked_by_copyrights"`
	}

	RawRelease *json.RawMessage

	ReleasesChunk    map[string]*Release
	RawReleasesChunk map[string]RawRelease
)

func (m *Release) IsBlockedInRegion(region string) (bool, bool) {
	if region == "" {
		return false, true
	}

	if m.BlockedInfo == nil || m.BlockedInfo.IsBlockedInGeo == nil {
		return false, false
	}

	for _, geo := range m.BlockedInfo.IsBlockedInGeo {
		if geo == region {
			return true, true
		}
	}

	return false, true
}

func (m *Release) IsOverworldBlocked() (bool, bool) {
	if m.BlockedInfo == nil {
		return false, false
	}

	return m.BlockedInfo.IsBlockedByCopyrights == true, true
}

// func (m *Release) RawJSON() (_ []byte, ok bool) {
// 	if len(m.raw) == 0 {
// 		ok = false
// 		return
// 	}

// 	return m.raw, ok
// }

func (m *Release) SetRawJSON(raw RawRelease) {
	m.raw = raw
}
