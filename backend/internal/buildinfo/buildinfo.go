// Package buildinfo 保存编译期注入的构建信息。
package buildinfo

// Version 由构建时的 ldflags 注入:
//
//	go build -ldflags "-X fanke/backend/internal/buildinfo.Version=v1.0.0"
//
// 未注入时为 "dev"。
var Version = "dev"
