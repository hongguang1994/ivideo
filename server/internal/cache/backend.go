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

// ShareEntry 是分享内的一个条目。
type ShareEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // 分享内相对路径，可直接用作资源的 FilePath
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// ShareLister 是可选能力：能列出分享内的目录，用于批量导入。
type ShareLister interface {
	ListShare(ctx context.Context, share ShareRef, subPath string) ([]ShareEntry, error)
}

// ShareSaver 是可选能力：把分享内某路径「手动转存」到自己盘的指定目录。
// 这条独立于按需转存缓存 —— 存进去的文件是永久留存，不受清理任务影响。
type ShareSaver interface {
	SaveToFolder(ctx context.Context, share ShareRef, srcPath, targetFolder string) error
}

// HLSStreamer 表示该适配器的 DirectURL 返回的是 HLS 播放列表(m3u8)。
// 不能靠 URL 里有没有 ".m3u8" 判断 —— 阿里 FHD/HD 档的地址路径里并不带该后缀。
type HLSStreamer interface {
	IsHLS() bool
}

// OriginalURLProvider 是可选能力：能给「原画直链」的适配器实现它。
// 用于 Emby/Jellyfin(strm) 这类能直接吃原始文件的客户端；
// 浏览器则走 DirectURL（转码 HLS），因为浏览器放不了 mkv。
type OriginalURLProvider interface {
	OriginalURL(ctx context.Context, cachePath string) (string, error)
}

// TokenVerifier 是可选能力：实测校验某类凭据当前是否有效(会真的向网盘发一次请求)。
// provider 取 "aliyun"(网页版 token)/ "aliyun_open"(开放接口 token)等。
type TokenVerifier interface {
	Verify(ctx context.Context, provider string) error
}
