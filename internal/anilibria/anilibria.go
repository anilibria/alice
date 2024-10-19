package anilibria

import "encoding/json"

type (
	RawReleases map[string]json.RawMessage
	Releases    map[string]*Release
	Release     struct {
		Id          uint
		Code        string
		BlockedInfo *ReleaseBlockedInfo `json:"blockedInfo"`
	}
	ReleaseBlockedInfo struct {
		Blocked               bool
		Reason                string
		IsBlockedInGeo        []string `json:"is_blocked_in_geo"`
		IsBlockedByCopyrights bool     `json:"is_blocked_by_copyrights"`
	}
)
