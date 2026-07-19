package backends

import (
	"context"

	"ivideo/server/internal/cache"
)

// Aliyun 是阿里云盘缓存盘适配器（当前为 stub 占位）。
//
// 落地实现要点（待填）：
//   - 转存第三方阿里分享，官方个人 OpenAPI 不支持，需用非官方网页 API。
//     Go 可直接用成熟库 github.com/tickstep/aliyunpan-api。
//   - 强烈建议用一个“专用小号”承载缓存，把封号风险隔离。
//   - 用 refreshToken（网页登录后获取）换 accessToken。
//   - Transfer：解析分享链接 → 转存到 cacheDir（同源转存≈秒传，瞬时）。
//   - DirectURL：对缓存文件取下载直链（注意直链有时效，可能要按需刷新）。
//   - Delete + EmptyTrash：删除并清空回收站，才真正释放配额。
//   - Quota：查询网盘空间用量，供 LRU 判断。
type Aliyun struct {
	refreshToken string
	cacheDir     string
}

// NewAliyun 创建阿里云盘适配器。
func NewAliyun(refreshToken, cacheDir string) *Aliyun {
	return &Aliyun{refreshToken: refreshToken, cacheDir: cacheDir}
}

func (a *Aliyun) Name() string { return "aliyun" }

func (a *Aliyun) Transfer(ctx context.Context, share cache.ShareRef) (cache.TransferResult, error) {
	return cache.TransferResult{}, cache.ErrNotImplemented
}

func (a *Aliyun) DirectURL(ctx context.Context, cachePath string) (string, error) {
	return "", cache.ErrNotImplemented
}

func (a *Aliyun) Delete(ctx context.Context, cachePath string) error {
	return cache.ErrNotImplemented
}

func (a *Aliyun) EmptyTrash(ctx context.Context) error {
	return cache.ErrNotImplemented
}

func (a *Aliyun) Quota(ctx context.Context) (used, total int64, err error) {
	return 0, 0, cache.ErrNotImplemented
}
