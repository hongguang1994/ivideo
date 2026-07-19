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

// Aliyun 是阿里云盘缓存盘适配器，实现 Share2Open 转存缓存：
//
//	分享 → (web接口)转存到自己盘临时目录 → (开放接口)取直链播放 → 看完删除
//
// 两套 token：
//   - web refresh token（小雅 mytoken.txt）：做「取分享token」「转存」。
//   - open refresh token（小雅 myopentoken.txt）：做「取直链」「删除」「查空间」。
//
// ⚠️ 注意：
//   - 阿里的 refresh token 每次刷新都会「轮换」。本适配器只在内存里保存轮换后的
//     token，进程重启后仍用配置里的初始 token —— 若该 token 已被轮换失效会登录失败。
//     生产使用建议接持久化，或用一个**专用小号**，避免和小雅共用同一 token 互相把对方挤下线。
//   - 转存目前只支持「秒传」同步返回；非秒传（返回 async_task_id）首版未做轮询。
type Aliyun struct {
	webRT        string
	openRT       string
	clientID     string
	clientSecret string
	openTokenURL string
	tempFolderID string
	driveID      string

	http *http.Client

	mu      sync.Mutex
	webTok  string
	webExp  time.Time
	openTok string
	openExp time.Time
}

// NewAliyun 从配置创建阿里云盘适配器。
func NewAliyun(cfg config.Config) *Aliyun {
	return &Aliyun{
		webRT:        cfg.AliyunRefreshToken,
		openRT:       cfg.AliyunOpenRefreshToken,
		clientID:     cfg.AliyunOpenClientID,
		clientSecret: cfg.AliyunOpenClientSecret,
		openTokenURL: cfg.AliyunOpenTokenURL,
		tempFolderID: cfg.AliyunTempFolderID,
		driveID:      cfg.AliyunDriveID,
		http:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *Aliyun) Name() string { return "aliyun" }

// ready 校验必要配置。
func (a *Aliyun) ready() error {
	if a.webRT == "" {
		return fmt.Errorf("阿里适配器缺少 ALIYUN_REFRESH_TOKEN（web refresh token）")
	}
	if a.openRT == "" || a.clientID == "" {
		return fmt.Errorf("阿里适配器缺少开放接口配置（ALIYUN_OPEN_REFRESH_TOKEN / ALIYUN_OPEN_CLIENT_ID 等）")
	}
	return nil
}

// Transfer 把分享内文件转存到自己盘临时目录。
func (a *Aliyun) Transfer(ctx context.Context, share cache.ShareRef) (cache.TransferResult, error) {
	if err := a.ready(); err != nil {
		return cache.TransferResult{}, err
	}
	shareID := parseAliShareID(share.ShareURL)
	if shareID == "" {
		return cache.TransferResult{}, fmt.Errorf("无法从分享链接解析 share_id: %s", share.ShareURL)
	}

	webTok, err := a.refreshWeb(ctx) // 同时确保 driveID 已就绪
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
	newID, err := a.copyFromShare(ctx, webTok, shareID, shareTok, fileID)
	if err != nil {
		return cache.TransferResult{}, err
	}
	return cache.TransferResult{CachePath: newID, Size: size}, nil
}

// DirectURL 用开放接口取已转存文件的直链。cachePath 即自己盘的 file_id。
func (a *Aliyun) DirectURL(ctx context.Context, cachePath string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	openTok, err := a.refreshOpen(ctx)
	if err != nil {
		return "", err
	}
	return a.openDownloadURL(ctx, openTok, cachePath)
}

// Delete 永久删除已转存文件。
func (a *Aliyun) Delete(ctx context.Context, cachePath string) error {
	if err := a.ready(); err != nil {
		return err
	}
	openTok, err := a.refreshOpen(ctx)
	if err != nil {
		return err
	}
	return a.openDelete(ctx, openTok, cachePath)
}

// EmptyTrash 采用永久删除，无需清回收站。
func (a *Aliyun) EmptyTrash(ctx context.Context) error { return nil }

// Quota 查询自己盘空间用量。
func (a *Aliyun) Quota(ctx context.Context) (used, total int64, err error) {
	if err = a.ready(); err != nil {
		return 0, 0, err
	}
	openTok, err := a.refreshOpen(ctx)
	if err != nil {
		return 0, 0, err
	}
	return a.openSpaceInfo(ctx, openTok)
}

// parseAliShareID 从分享链接里取 share_id，例如
// https://www.alipan.com/s/wtT3hMC4vti → wtT3hMC4vti
func parseAliShareID(shareURL string) string {
	s := shareURL
	if i := strings.Index(s, "/s/"); i >= 0 {
		s = s[i+3:]
	}
	// 去掉后面的查询串或路径
	for _, sep := range []string{"?", "/", "#"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}
