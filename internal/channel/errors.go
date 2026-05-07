package channel

import "errors"

var (
	// ErrNoChannelAvailable 无可用的渠道
	ErrNoChannelAvailable = errors.New("无可用的渠道")
	// ErrChannelDisabled 渠道已禁用
	ErrChannelDisabled = errors.New("渠道已禁用")
	// ErrChannelNotFound 渠道不存在
	ErrChannelNotFound = errors.New("渠道不存在")
)
