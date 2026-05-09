package i18n

// T translates a key to the given language
func T(lang, key string) string {
	messages := map[string]map[string]string{
		"zh": {
			"unauthorized":          "未授权",
			"forbidden":             "禁止访问",
			"invalid_request":       "无效的请求",
			"not_found":             "资源不存在",
			"internal_error":        "服务器内部错误",
			"method_not_allowed":    "方法不允许",
			"bad_request":           "请求格式错误",
			"login_required":        "请先登录",
			"invalid_credentials":   "用户名或密码错误",
			"channel_not_found":     "渠道不存在",
			"route_not_found":       "路由不存在",
			"no_available_channel":  "没有可用的渠道",
			"model_not_allowed":     "该渠道不允许使用此模型",
			"invalid_json":          "JSON 格式无效",
			"model_required":        "模型名称不能为空",
			"service_unavailable":   "服务暂时不可用",
			"channel_disabled":      "渠道已禁用",
			"all_channels_failed":   "所有渠道都失败了",
			"key_not_found":         "Key 不存在",
			"channel_resumed":       "渠道已恢复",
			"operation_success":     "操作成功",
			"invalid_id":            "无效的 ID",
			"id_required":           "ID 不能为空",
			"timeout":               "请求超时",
			"rate_limit":            "请求过于频繁",
			"database_error":        "数据库错误",
			"config_error":          "配置错误",
		},
		"en": {
			"unauthorized":          "Unauthorized",
			"forbidden":             "Forbidden",
			"invalid_request":       "Invalid Request",
			"not_found":             "Not Found",
			"internal_error":        "Internal Server Error",
			"method_not_allowed":    "Method Not Allowed",
			"bad_request":           "Bad Request",
			"login_required":        "Login Required",
			"invalid_credentials":   "Invalid Username or Password",
			"channel_not_found":     "Channel Not Found",
			"route_not_found":       "Route Not Found",
			"no_available_channel":  "No Available Channel",
			"model_not_allowed":     "Model Not Allowed on This Channel",
			"invalid_json":          "Invalid JSON Format",
			"model_required":        "Model Name is Required",
			"service_unavailable":   "Service Temporarily Unavailable",
			"channel_disabled":      "Channel Disabled",
			"all_channels_failed":   "All Channels Failed",
			"key_not_found":         "Key Not Found",
			"channel_resumed":       "Channel Resumed",
			"operation_success":     "Operation Successful",
			"invalid_id":            "Invalid ID",
			"id_required":           "ID is Required",
			"timeout":               "Request Timeout",
			"rate_limit":            "Rate Limit Exceeded",
			"database_error":        "Database Error",
			"config_error":          "Configuration Error",
		},
	}

	if msgs, ok := messages[lang]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}

	// Fallback to English, then to key itself
	if msgs, ok := messages["en"]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	return key
}

// ErrorCode returns a structured error response
func ErrorCode(lang, code string, httpStatus int) (string, string, int) {
	return code, T(lang, code), httpStatus
}
