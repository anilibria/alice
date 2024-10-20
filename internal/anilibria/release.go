package anilibria

import (
	"github.com/goccy/go-json"
)

type (
	Release struct {
		Id          uint
		Code        string
		BlockedInfo *ReleaseBlockedInfo `json:"blockedInfo"`

		raw []byte
	}
	ReleaseBlockedInfo struct {
		Blocked               bool     `json:"blocked"`
		Reason                string   `json:"reason"`
		IsBlockedInGeo        []string `json:"is_blocked_in_geo"`
		IsBlockedByCopyrights bool     `json:"is_blocked_by_copyrights"`
	}

	RawRelease map[string]*json.RawMessage

	ReleasesChunk    map[string]*Release
	RawReleasesChunk map[string]RawRelease
)

type ReleaseBlockReason string

const (
	// WW block
	BlockedByCopyrights = "Данный релиз заблокирован во всех регионах по требованию правообладателя."
	// Regional block
	BlockedInRegion = "Данный релиз заблокирован в вашем регионе по требованию правоохранительных органов."
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

func (m *Release) Raw() ([]byte, bool) {
	if len(m.raw) == 0 {
		return nil, false
	}

	return m.raw, true
}

func (m *Release) SetRaw(raw []byte) {
	m.raw = raw
}

func (m *Release) PatchBlockReason(reason string) bool {
	if m.BlockedInfo == nil {
		return false
	}

	m.BlockedInfo.Reason = reason
	return true
}
