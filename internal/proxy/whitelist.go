package proxy

// var getArgsWhitelist = map[string]interface{}{
// 	"query": nil,
// }

// var actionWhitelist = map[string]interface{}{
// 	"add":    nil,
// 	"delete": nil,
// }

var postArgsWhitelist = map[string]interface{}{
	"action":  nil,
	"code":    nil,
	"filter":  nil,
	"id":      nil,
	"json":    nil,
	"page":    nil,
	"perPage": nil,
	"query":   nil,
	"rm":      nil,
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
