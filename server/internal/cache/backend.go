// Package cache 实现“按需转存 + 到期清理”的缓存层：
// 播放时把分享资源转存进自己网盘，播放走自己网盘直链，闲置后清理。
package cache

import (
	"context"
	"errors"
)

// ErrNotImplemented 供尚未实现的适配器返回。
var ErrNotImplemented = errors.New("该缓存盘适配器尚未实现")

// ShareRef 描述一个待转存的分享资源。
type ShareRef struct {
	Provider string // 源网盘类型：aliyun / pikpak / ...
	ShareURL string // 分享链接
	SharePwd string // 提取码（可选）
	FilePath string // 分享内具体文件（可选）
}

// TransferResult 是转存完成后的结果。
type TransferResult struct {
	CachePath string // 文件在自己网盘里的路径
	Size      int64  // 字节
}

// CacheBackend 是“缓存盘”适配器接口，每个网盘实现一套。
// 关键点：同源转存（源与缓存盘同为一家）应是近乎瞬时的元数据操作。
type CacheBackend interface {
	// Name 返回适配器名（与 CACHE_BACKEND 对应）。
	Name() string
	// Transfer 把分享转存进自己网盘的缓存目录。
	Transfer(ctx context.Context, share ShareRef) (TransferResult, error)
	// DirectURL 返回缓存文件的可播直链。
	DirectURL(ctx context.Context, cachePath string) (string, error)
	// Delete 删除缓存文件。
	Delete(ctx context.Context, cachePath string) error
	// EmptyTrash 清空回收站，真正释放配额。
	EmptyTrash(ctx context.Context) error
	// Quota 返回已用 / 总量（字节）。未知可返回 (0, 0, nil)。
	Quota(ctx context.Context) (used, total int64, err error)
}
