package backends

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
)

// TokenStore 供适配器读写持久化的网盘凭据（扫码写入的 refresh_token，轮换后回写）。
type TokenStore interface {
	GetToken(provider string) string
	SaveToken(provider, token string) error
}

// Aliyun 是阿里云盘缓存盘适配器（Share2Open 转存缓存，纯 web 单套）：
//
//	分享 → 转存到自己盘临时目录 → 取直链播放 → 删除
//
// token 来自「设置页扫码登录」写入数据库的 web refresh_token（轮换后自动回写库）。
// 若库里没有则回退到配置 ALIYUN_REFRESH_TOKEN。
//
// ⚠️ 已知限制：转存目前只支持秒传同步返回；非秒传（async_task_id）首版未做轮询。
type Aliyun struct {
	webRT        string // 配置里的初始 refresh token（回退用）
	tempFolderID string
	driveID      string
	tokens       TokenStore // 可为 nil

	http *http.Client

	mu        sync.Mutex
	accessTok string
	accessExp time.Time
}

// NewAliyun 从配置创建阿里云盘适配器；tokens 用于读写扫码后的持久化 token。
func NewAliyun(cfg config.Config, tokens TokenStore) *Aliyun {
	return &Aliyun{
		webRT:        cfg.AliyunRefreshToken,
		tempFolderID: cfg.AliyunTempFolderID,
		driveID:      cfg.AliyunDriveID,
		tokens:       tokens,
		http:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *Aliyun) Name() string { return "aliyun" }

// Transfer 把分享内文件转存到自己盘临时目录。
func (a *Aliyun) Transfer(ctx context.Context, share cache.ShareRef) (cache.TransferResult, error) {
	shareID := parseAliShareID(share.ShareURL)
	if shareID == "" {
		return cache.TransferResult{}, fmt.Errorf("无法从分享链接解析 share_id: %s", share.ShareURL)
	}
	accessTok, err := a.webToken(ctx) // 同时确保 driveID 就绪
	if err != nil {
		return cache.TransferResult{}, err
	}
	shareTok, err := a.shareToken(ctx, shareID, share.SharePwd)
	if err != nil {
		return cache.TransferResult{}, err
	}
	fileID, size, err := a.resolveFileID(ctx, shareID, shareTok, share.FilePath)
	if err != nil {
		return cache.TransferResult{}, err
	}
	newID, err := a.copyFromShare(ctx, accessTok, shareID, shareTok, fileID)
	if err != nil {
		return cache.TransferResult{}, err
	}
	return cache.TransferResult{CachePath: newID, Size: size}, nil
}

// DirectURL 取已转存文件的直链。cachePath 即自己盘 file_id。
func (a *Aliyun) DirectURL(ctx context.Context, cachePath string) (string, error) {
	accessTok, err := a.webToken(ctx)
	if err != nil {
		return "", err
	}
	return a.downloadURL(ctx, accessTok, cachePath)
}

// Delete 删除已转存文件（进回收站）。
func (a *Aliyun) Delete(ctx context.Context, cachePath string) error {
	accessTok, err := a.webToken(ctx)
	if err != nil {
		return err
	}
	return a.deleteFile(ctx, accessTok, cachePath)
}

// EmptyTrash 清空回收站，真正释放配额。
func (a *Aliyun) EmptyTrash(ctx context.Context) error {
	accessTok, err := a.webToken(ctx)
	if err != nil {
		return err
	}
	return a.clearTrash(ctx, accessTok)
}

// Quota 首版不查空间（清理按缓存项累计大小判断，不依赖此值）。
func (a *Aliyun) Quota(ctx context.Context) (used, total int64, err error) {
	return 0, 0, nil
}

// parseAliShareID 从分享链接取 share_id，例如
// https://www.alipan.com/s/wtT3hMC4vti → wtT3hMC4vti
func parseAliShareID(shareURL string) string {
	s := shareURL
	if i := strings.Index(s, "/s/"); i >= 0 {
		s = s[i+3:]
	}
	for _, sep := range []string{"?", "/", "#"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}
