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
	// GetTokenExtra 取凭据的附加信息。阿里开放接口用它存 driver 类型：
	//   alicloud_qr —— OAuth2 扫码(普通第三方应用，**阿里会限速**)
	//   alicloud_tv —— TV 版客户端(实测原画速度约快 5 倍)
	GetTokenExtra(provider string) string
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

	// 开放接口(取原画直链)
	openRT           string
	openClientID     string
	openClientSecret string
	openTokenURL     string
	openRenewURL     string
	openConnectorURL string

	// 各接口基础域名(端点路径在代码里拼接) + 伪装 UA
	apiBase   string
	authBase  string
	openBase  string
	userBase  string
	browserUA string

	http *http.Client

	mu        sync.Mutex
	accessTok string
	accessExp time.Time
	openTok   string
	openExp   time.Time

	// 分享目录列表缓存：同一目录短时间内重复列取直接命中，
	// 明显减少阿里调用（浏览/导入时最有效），也就更不容易触发 429。
	listMu    sync.Mutex
	listCache map[string]listCacheEntry
	listTTL   time.Duration
}

// listCacheEntry 是一条目录列表缓存。
type listCacheEntry struct {
	items []shareItem
	exp   time.Time
}

// NewAliyun 从配置创建阿里云盘适配器；tokens 用于读写扫码后的持久化 token。
func NewAliyun(cfg config.Config, tokens TokenStore) *Aliyun {
	return &Aliyun{
		webRT:            cfg.AliyunRefreshToken,
		tempFolderID:     cfg.AliyunTempFolderID,
		driveID:          cfg.AliyunDriveID,
		tokens:           tokens,
		openRT:           cfg.AliyunOpenRefreshToken,
		openClientID:     cfg.AliyunOpenClientID,
		openClientSecret: cfg.AliyunOpenClientSecret,
		openTokenURL:     cfg.AliyunOpenTokenURL,
		openRenewURL:     cfg.AliyunOpenRenewURL,
		openConnectorURL: cfg.AliyunOpenConnectorURL,
		apiBase:          cfg.AliyunAPIBase,
		authBase:         cfg.AliyunAuthBase,
		openBase:         cfg.AliyunOpenBase,
		userBase:         cfg.AliyunUserBase,
		browserUA:        cfg.AliyunBrowserUA,
		http:             &http.Client{Timeout: 30 * time.Second},
		listCache:        make(map[string]listCacheEntry),
		listTTL:          time.Duration(cfg.AliyunListCacheSeconds) * time.Second,
	}
}

// listCacheGet 取缓存的目录列表；未命中或已过期返回 ok=false。
func (a *Aliyun) listCacheGet(key string) ([]shareItem, bool) {
	if a.listTTL <= 0 {
		return nil, false
	}
	a.listMu.Lock()
	defer a.listMu.Unlock()
	e, ok := a.listCache[key]
	if !ok || time.Now().After(e.exp) {
		return nil, false
	}
	return e.items, true
}

// listCachePut 写入目录列表缓存；超过容量上限时先清过期项，仍超则整体清空（简单够用）。
func (a *Aliyun) listCachePut(key string, items []shareItem) {
	if a.listTTL <= 0 {
		return
	}
	a.listMu.Lock()
	defer a.listMu.Unlock()
	const maxEntries = 2000
	if len(a.listCache) >= maxEntries {
		now := time.Now()
		for k, e := range a.listCache {
			if now.After(e.exp) {
				delete(a.listCache, k)
			}
		}
		if len(a.listCache) >= maxEntries {
			a.listCache = make(map[string]listCacheEntry, maxEntries)
		}
	}
	a.listCache[key] = listCacheEntry{items: items, exp: time.Now().Add(a.listTTL)}
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
	return a.playURL(ctx, accessTok, cachePath)
}

// ListShare 列出分享内某目录(subPath 为空则列根)。
func (a *Aliyun) ListShare(ctx context.Context, share cache.ShareRef, subPath string) ([]cache.ShareEntry, error) {
	shareID := parseAliShareID(share.ShareURL)
	if shareID == "" {
		return nil, fmt.Errorf("无法解析 share_id: %s", share.ShareURL)
	}
	shareTok, err := a.shareToken(ctx, shareID, share.SharePwd)
	if err != nil {
		return nil, err
	}
	parent := "root"
	if subPath != "" && subPath != "/" {
		id, _, err := a.resolveFileID(ctx, shareID, shareTok, subPath)
		if err != nil {
			return nil, err
		}
		parent = id
	}
	items, err := a.listShare(ctx, shareID, shareTok, parent)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(subPath, "/")
	out := make([]cache.ShareEntry, 0, len(items))
	for _, it := range items {
		out = append(out, cache.ShareEntry{
			Name:  it.Name,
			Path:  base + "/" + it.Name,
			IsDir: it.Type == "folder",
			Size:  it.Size,
		})
	}
	return out, nil
}

// SaveToFolder 把分享内 srcPath(文件或文件夹)手动转存到自己盘的 targetFolder(默认 ivideo)。
// 永久留存,独立于按需转存缓存,不会被清理任务删除。
func (a *Aliyun) SaveToFolder(ctx context.Context, share cache.ShareRef, srcPath, targetFolder string) error {
	if strings.TrimSpace(srcPath) == "" {
		return fmt.Errorf("未指定要转存的文件路径")
	}
	if strings.TrimSpace(targetFolder) == "" {
		targetFolder = "ivideo"
	}
	shareID := parseAliShareID(share.ShareURL)
	if shareID == "" {
		return fmt.Errorf("无法解析 share_id: %s", share.ShareURL)
	}
	accessTok, err := a.webToken(ctx) // 同时确保 driveID 就绪
	if err != nil {
		return err
	}
	shareTok, err := a.shareToken(ctx, shareID, share.SharePwd)
	if err != nil {
		return err
	}
	fileID, _, err := a.resolveFileID(ctx, shareID, shareTok, srcPath)
	if err != nil {
		return err
	}
	targetID, err := a.ensureFolderPath(ctx, accessTok, targetFolder)
	if err != nil {
		return err
	}
	_, err = a.copyShareItemTo(ctx, accessTok, shareID, shareTok, fileID, targetID)
	return err
}

// WalkShare 高效遍历整个分享,返回所有【文件】条目。
// 只换一次 share_token、按 file_id 递归(不像逐目录 ListShare 每次重取 token+重解析路径),
// API 调用量从 O(目录数×深度) 降到 O(目录数),避免阿里 429 限流;并对 429 指数退避重试。
func (a *Aliyun) WalkShare(ctx context.Context, share cache.ShareRef) ([]cache.ShareEntry, error) {
	shareID := parseAliShareID(share.ShareURL)
	if shareID == "" {
		return nil, fmt.Errorf("无法解析 share_id: %s", share.ShareURL)
	}
	shareTok, err := a.shareToken(ctx, shareID, share.SharePwd)
	if err != nil {
		return nil, err
	}
	// 起点:默认分享根;share.FilePath 指定了子目录则从那里开始(精简导入某目录)。
	startID, prefix := "root", ""
	if sp := strings.TrimRight(share.FilePath, "/"); sp != "" {
		id, _, err := a.resolveFileID(ctx, shareID, shareTok, sp)
		if err != nil {
			return nil, fmt.Errorf("解析起始目录 %q 失败: %w", sp, err)
		}
		startID, prefix = id, sp
	}
	var out []cache.ShareEntry
	err = a.walkShareByID(ctx, shareID, shareTok, startID, prefix, 0, &out)
	return out, err
}

// walkShareByID 按 file_id 递归收集文件。子目录失败(重试后仍限流)则跳过,不中断整体。
func (a *Aliyun) walkShareByID(ctx context.Context, shareID, shareTok, parentID, prefix string, depth int, out *[]cache.ShareEntry) error {
	if depth > 30 { // 安全上限,防异常深度/循环挂载
		return nil
	}
	// 重试退避已下沉到 doJSON（429/408/5xx 统一处理），这里直接调即可。
	items, err := a.listShare(ctx, shareID, shareTok, parentID)
	if err != nil {
		return err
	}
	for _, it := range items {
		p := prefix + "/" + it.Name
		if it.Type == "folder" {
			// 单枝失败不致命:跳过继续走其余。
			_ = a.walkShareByID(ctx, shareID, shareTok, it.FileID, p, depth+1, out)
			continue
		}
		*out = append(*out, cache.ShareEntry{Name: it.Name, Path: p, Size: it.Size})
	}
	return nil
}

// IsHLS 阿里的 DirectURL 返回的是转码 HLS 播放列表。
func (a *Aliyun) IsHLS() bool { return true }

// Verify 实测校验凭据是否有效:尝试换取访问令牌,成功即有效。
//   - aliyun:刷新网页版 token(access 已缓存且未过期时不触发刷新/轮换)
//   - aliyun_open:刷新开放接口 token
func (a *Aliyun) Verify(ctx context.Context, provider string) error {
	switch provider {
	case "aliyun", "":
		_, err := a.webToken(ctx)
		return err
	case "aliyun_open":
		_, err := a.openAccessToken(ctx)
		return err
	default:
		return fmt.Errorf("阿里适配器不支持校验 provider: %s", provider)
	}
}

// OriginalURL 用开放接口取「原画直链」(mkv/mp4 本体,支持 Range)。
// 供 Emby/Jellyfin(strm) 使用；浏览器仍用 DirectURL 的转码 HLS。
func (a *Aliyun) OriginalURL(ctx context.Context, cachePath string) (string, error) {
	if _, err := a.webToken(ctx); err != nil { // 确保 driveID 就绪
		return "", err
	}
	openTok, err := a.openAccessToken(ctx)
	if err != nil {
		return "", err
	}
	return a.originalURL(ctx, openTok, cachePath)
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
