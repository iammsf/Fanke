// Package buildinfo 保存编译期注入的构建信息。
package buildinfo

// Version 由构建时的 ldflags 注入(构建时间戳,如 202611150235):
//
//	go build -ldflags "-X fanke/backend/internal/buildinfo.Version=202611150235"
//
// 未注入时为 "dev"。
var Version = "dev"
