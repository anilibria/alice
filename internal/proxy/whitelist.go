package proxy

// var getArgsWhitelist = map[string]interface{}{
// 	"query": nil,
// }

// var actionWhitelist = map[string]interface{}{
// 	"add":    nil,
// 	"delete": nil,
// }

var postArgsWhitelist = map[string]interface{}{
	// POST api.php
	"action":  nil,
	"code":    nil,
	"filter":  nil,
	"id":      nil,
	"json":    nil,
	"page":    nil,
	"perPage": nil,
	"query":   nil,
	"rm":      nil,

	// POST func.php - most used
	"sort":    nil,
	"xpage":   nil,
	"finish":  nil,
	"search":  nil,
	"current": nil,

	// POST func.php
	"2fa":                  nil,
	"announce":             nil,
	"csrf":                 nil,
	"csrf_token":           nil,
	"data":                 nil,
	"deviceId":             nil,
	"do":                   nil,
	"fa2code":              nil,
	"g-recaptcha-response": nil,
	"info":                 nil,
	"key":                  nil,
	"login":                nil,
	"mail":                 nil,
	"mes":                  nil,
	"newPasswd":            nil,
	"oldPasswd":            nil,
	"order0column":         nil,
	"order0dir":            nil,
	"passwd":               nil,
	"recaptcha":            nil,
	"reset":                nil,
	"rid":                  nil,
	"searchvalue":          nil,
	"url":                  nil,
	"v":                    nil,
	"vk":                   nil,
	"w":                    nil,
	"width":                nil,
}

var queryWhitelist = map[string]interface{}{
	// GET
	"api_empty": nil,

	// POST api.php top
	"app_update":       nil,
	"config":           nil,
	"donation_details": nil,
	"empty":            nil,

	// POST api.php bottom
	"teams":           nil,
	"torrent":         nil,
	"info":            nil,
	"franchises":      nil,
	"release":         nil,
	"random_release":  nil,
	"list":            nil,
	"schedule":        nil,
	"feed":            nil,
	"genres":          nil,
	"years":           nil,
	"favorites":       nil,
	"youtube":         nil,
	"user":            nil,
	"catalog":         nil,
	"search":          nil,
	"vkcomments":      nil,
	"social_auth":     nil,
	"link_menu":       nil,
	"reserved_test":   nil,
	"auth_get_otp":    nil,
	"auth_accept_otp": nil,
	"auth_login_otp":  nil,
}
