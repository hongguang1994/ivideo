package backends

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
)

// 夸克网盘适配器（全链路走网页 cookie）。
//
// 为什么不用开放平台：夸克开放 API（open-api-drive.quark.cn）要求公参
// x-pan-client-id / x-pan-tm / x-pan-token，其中 x-pan-token 是用应用 secret 算的签名；
// 在线中转（oplist）只给 access_token 不给 secret，因此拿中转令牌调不动开放 API（实测）。
//
// 转存协议（参考 cloudsaver 的实现，端点与流程均已实测验证）：
//  1. share/sharepage/token   pwd_id + passcode → stoken
//  2. share/sharepage/detail  stoken + pdir_fid → 列分享内容（每项带 share_fid_token）
//  3. share/sharepage/save    fid_list + fid_token_list + stoken → 转存进自己盘
//
// 已知限制：下载直链有强防盗链，实测各种 UA/Cookie/Referer 组合均返回 412，
// 故 DirectURL 暂不可用（播放待后续攻克；转存进自己盘后可用夸克 App 观看）。
const (
	quarkShareBase = "https://drive-h.quark.cn"  // 分享相关（token/detail/save）
	quarkDriveBase = "https://drive-pc.quark.cn" // 自己盘（列目录/取直链/删除）
	// QuarkUA 是夸克官方桌面客户端 UA（取自 alist 实现）。夸克接口对 UA 敏感。
	QuarkUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch"
)

// reQuarkShare 从分享链接解析 pwd_id：https://pan.quark.cn/s/<pwd_id>
var reQuarkShare = regexp.MustCompile(`pan\.quark\.cn/s/([A-Za-z0-9]+)`)

// Quark 是夸克网盘缓存盘适配器。
type Quark struct {
	http   *http.Client
	tokens TokenStore

	// 夸克每次调 download 都会下发新的 __puus cookie，**拉流必须用刷新后的值**，
	// 否则 CDN 返回 412（这正是之前播放不通的原因）。这里缓存最近一次刷新结果，
	// 供播放代理取用。
	streamMu     sync.Mutex
	streamCookie string

	cacheDirMu  sync.Mutex
	cacheDirFid string // 缓存目录(默认 ivideo)的 fid，首次用时解析并记住

	// 直链缓存：ffprobe 探测/播放器 seek 会**反复**取直链，每次都请求夸克
	// download 接口会把开播拖到近 20 秒（实测）。夸克直链 auth_key 有效期约 6h，
	// 这里短时缓存即可大幅提速。
	urlMu    sync.Mutex
	urlCache map[string]quarkCachedURL
}

type quarkCachedURL struct {
	url string
	exp time.Time
}

// quarkURLTTL 是直链缓存时长（远小于夸克 auth_key 的 6h 有效期，安全）。
const quarkURLTTL = 5 * time.Minute

// StreamCookie 返回拉流应使用的 cookie（download 刷新过的）。
// 播放代理用它请求直链 —— 夸克直链绑 cookie，不能 302 直跳给播放器。
func (q *Quark) StreamCookie() string {
	q.streamMu.Lock()
	defer q.streamMu.Unlock()
	if q.streamCookie != "" {
		return q.streamCookie
	}
	return q.cookie()
}

// NewQuark 创建夸克适配器。cookie 从 TokenStore(provider="quark_cookie") 读，
// 由「设置页夸克扫码登录」写入。
func NewQuark(cfg config.Config, tokens TokenStore) *Quark {
	return &Quark{
		http:     &http.Client{Timeout: 30 * time.Second},
		tokens:   tokens,
		urlCache: make(map[string]quarkCachedURL),
	}
}

// Name 实现 CacheBackend。
func (q *Quark) Name() string { return "quark" }

// cookie 读网页态 cookie。
func (q *Quark) cookie() string {
	if q.tokens != nil {
		return q.tokens.GetToken("quark_cookie")
	}
	return ""
}

// call 发夸克 API 请求（自动补公参、带 cookie 与客户端 UA）。
func (q *Quark) call(ctx context.Context, method, base, path string, query url.Values, payload any, out any) error {
	ck := q.cookie()
	if ck == "" {
		return fmt.Errorf("未配置夸克 cookie，请在设置页扫码登录")
	}
	if query == nil {
		query = url.Values{}
	}
	// 夸克必需的公参。
	query.Set("pr", "ucpro")
	query.Set("fr", "pc")
	query.Set("uc_param_str", "")
	query.Set("__t", fmt.Sprintf("%d", time.Now().UnixMilli()))

	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path+"?"+query.Encode(), body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", QuarkUA)
	req.Header.Set("Cookie", ck)
	req.Header.Set("Referer", "https://pan.quark.cn/")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := q.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(raw, out)
}

// quarkShareItem 是分享内的一个条目。
type quarkShareItem struct {
	Fid           string `json:"fid"`
	FileName      string `json:"file_name"`
	Dir           bool   `json:"dir"`
	Size          int64  `json:"size"`
	ShareFidToken string `json:"share_fid_token"`
}

// shareToken 用 pwd_id + 提取码换 stoken（后续 detail/save 都要它）。
func (q *Quark) shareToken(ctx context.Context, pwdID, passcode string) (string, error) {
	var out struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Stoken string `json:"stoken"`
		} `json:"data"`
	}
	err := q.call(ctx, http.MethodPost, quarkShareBase, "/1/clouddrive/share/sharepage/token",
		nil, map[string]string{"pwd_id": pwdID, "passcode": passcode}, &out)
	if err != nil {
		return "", err
	}
	if out.Data.Stoken == "" {
		return "", fmt.Errorf("夸克取分享 token 失败: %s", out.Message)
	}
	return out.Data.Stoken, nil
}

// shareDetail 列分享内某目录（pdirFid 为 "0" 表示分享根）。
func (q *Quark) shareDetail(ctx context.Context, pwdID, stoken, pdirFid string) ([]quarkShareItem, error) {
	var out struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    struct {
			List []quarkShareItem `json:"list"`
		} `json:"data"`
	}
	qs := url.Values{
		"pwd_id":   {pwdID},
		"stoken":   {stoken}, // Encode() 会做 URL 编码 —— 不编码夸克会报「非法token」
		"pdir_fid": {pdirFid},
		"force":    {"0"},
		"_page":    {"1"},
		"_size":    {"200"},
	}
	if err := q.call(ctx, http.MethodGet, quarkShareBase, "/1/clouddrive/share/sharepage/detail",
		qs, nil, &out); err != nil {
		return nil, err
	}
	if out.Status != 200 {
		return nil, fmt.Errorf("夸克浏览分享失败: %s", out.Message)
	}
	return out.Data.List, nil
}

// shareSave 把分享内文件转存进自己盘目录 toDir（"0" 为根目录）。
func (q *Quark) shareSave(ctx context.Context, pwdID, stoken string, it quarkShareItem, toDir string) error {
	var out struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	}
	payload := map[string]any{
		"fid_list":       []string{it.Fid},
		"fid_token_list": []string{it.ShareFidToken},
		"to_pdir_fid":    toDir,
		"pwd_id":         pwdID,
		"stoken":         stoken,
		"pdir_fid":       "0",
		"scene":          "link",
	}
	if err := q.call(ctx, http.MethodPost, quarkShareBase, "/1/clouddrive/share/sharepage/save",
		nil, payload, &out); err != nil {
		return err
	}
	if out.Status != 200 {
		return fmt.Errorf("夸克转存失败: %s", out.Message)
	}
	return nil
}

// findInShare 在分享里按路径（形如 "目录/子目录/文件名"）逐层定位一个文件。
func (q *Quark) findInShare(ctx context.Context, pwdID, stoken, filePath string) (quarkShareItem, error) {
	segs := make([]string, 0, 4)
	for _, s := range strings.Split(filePath, "/") {
		if s = strings.TrimSpace(s); s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return quarkShareItem{}, fmt.Errorf("夸克资源缺少文件路径")
	}
	pdir := "0"
	for i, want := range segs {
		items, err := q.shareDetail(ctx, pwdID, stoken, pdir)
		if err != nil {
			return quarkShareItem{}, err
		}
		var hit *quarkShareItem
		for idx := range items {
			if items[idx].FileName == want {
				hit = &items[idx]
				break
			}
		}
		if hit == nil {
			return quarkShareItem{}, fmt.Errorf("夸克分享内未找到 %q", want)
		}
		if i == len(segs)-1 {
			return *hit, nil
		}
		pdir = hit.Fid
	}
	return quarkShareItem{}, fmt.Errorf("夸克分享内未找到 %q", filePath)
}

// ---- CacheBackend ----

// Transfer 按需转存：把分享里的目标文件存进自己盘根目录，返回其 fid。
func (q *Quark) Transfer(ctx context.Context, share cache.ShareRef) (cache.TransferResult, error) {
	// 无分享链接：视为「文件已在自己盘」，FilePath 直接是 fid（导入自己盘的资源）。
	if strings.TrimSpace(share.ShareURL) == "" {
		fid := strings.TrimSpace(share.FilePath)
		if fid == "" {
			return cache.TransferResult{}, fmt.Errorf("夸克资源缺少 fid")
		}
		return cache.TransferResult{CachePath: fid}, nil
	}
	m := reQuarkShare.FindStringSubmatch(share.ShareURL)
	if m == nil {
		return cache.TransferResult{}, fmt.Errorf("无法解析夸克分享链接: %s", share.ShareURL)
	}
	pwdID := m[1]
	stoken, err := q.shareToken(ctx, pwdID, strings.TrimSpace(share.SharePwd))
	if err != nil {
		return cache.TransferResult{}, err
	}
	it, err := q.findInShare(ctx, pwdID, stoken, share.FilePath)
	if err != nil {
		return cache.TransferResult{}, err
	}
	// 转存进专用缓存目录（默认 ivideo），不污染网盘根目录。
	cacheDir, err := q.cacheFolderFid(ctx)
	if err != nil {
		return cache.TransferResult{}, err
	}
	// 已经转存过就直接复用（Jellyfin 扫库/探测会反复触发，重复转存既慢又可能被夸克限流）。
	if fid, size, err := q.findInFolderOnce(ctx, cacheDir, it.FileName); err == nil && fid != "" {
		return cache.TransferResult{CachePath: fid, Size: size}, nil
	}
	if err := q.shareSave(ctx, pwdID, stoken, it, cacheDir); err != nil {
		return cache.TransferResult{}, err
	}
	// 转存是异步任务，稍后在缓存目录里按文件名找到 fid。
	fid, size, err := q.findInFolder(ctx, cacheDir, it.FileName)
	if err != nil {
		return cache.TransferResult{}, err
	}
	return cache.TransferResult{CachePath: fid, Size: size}, nil
}

// findInFolder 在指定目录按文件名找 fid（转存后定位用，带重试等异步任务落地）。
func (q *Quark) findInFolder(ctx context.Context, dirFid, name string) (string, int64, error) {
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		var out struct {
			Code int `json:"code"`
			Data struct {
				List []struct {
					Fid      string `json:"fid"`
					FileName string `json:"file_name"`
					Size     int64  `json:"size"`
				} `json:"list"`
			} `json:"data"`
		}
		qs := url.Values{"pdir_fid": {dirFid}, "_page": {"1"}, "_size": {"200"},
			"_sort": {"file_type:asc,updated_at:desc"}}
		if err := q.call(ctx, http.MethodGet, quarkDriveBase, "/1/clouddrive/file/sort", qs, nil, &out); err != nil {
			return "", 0, err
		}
		for _, f := range out.Data.List {
			if f.FileName == name {
				return f.Fid, f.Size, nil
			}
		}
	}
	return "", 0, fmt.Errorf("夸克转存后未在自己盘找到 %q", name)
}

// DirectURL 取播放直链。
// 注意：夸克直链有强防盗链，实测服务端拉流恒返回 412（各种 UA/Cookie/Referer 组合均无效），
// 故此处返回明确错误而非一个不可用的地址，避免上层反复重试。
func (q *Quark) DirectURL(ctx context.Context, cachePath string) (string, error) {
	// 先看缓存 —— 探测和 seek 会高频调这里。
	q.urlMu.Lock()
	if c, ok := q.urlCache[cachePath]; ok && time.Now().Before(c.exp) {
		u := c.url
		q.urlMu.Unlock()
		return u, nil
	}
	q.urlMu.Unlock()

	ck := q.cookie()
	if ck == "" {
		return "", fmt.Errorf("未配置夸克 cookie，请在设置页扫码登录")
	}
	body, _ := json.Marshal(map[string]any{"fids": []string{cachePath}})
	qs := url.Values{"pr": {"ucpro"}, "fr": {"pc"}, "uc_param_str": {""},
		"__t": {fmt.Sprintf("%d", time.Now().UnixMilli())}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		quarkDriveBase+"/1/clouddrive/file/download?"+qs.Encode(), strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", QuarkUA)
	req.Header.Set("Cookie", ck)
	req.Header.Set("Referer", "https://pan.quark.cn/")
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// **关键**：把响应里刷新的 cookie（尤其 __puus）合并进去，供拉流使用。
	q.mergeStreamCookie(ck, resp.Cookies())

	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    []struct {
			DownloadURL string `json:"download_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Code != 0 || len(out.Data) == 0 || out.Data[0].DownloadURL == "" {
		return "", fmt.Errorf("夸克取直链失败: %s", out.Message)
	}
	u := out.Data[0].DownloadURL
	q.urlMu.Lock()
	q.urlCache[cachePath] = quarkCachedURL{url: u, exp: time.Now().Add(quarkURLTTL)}
	q.urlMu.Unlock()
	return u, nil
}

// mergeStreamCookie 用响应下发的 Set-Cookie 覆盖原 cookie 里的同名项，存为拉流 cookie。
func (q *Quark) mergeStreamCookie(base string, cookies []*http.Cookie) {
	kv := map[string]string{}
	order := []string{}
	for _, part := range strings.Split(base, "; ") {
		if i := strings.Index(part, "="); i > 0 {
			k := strings.TrimSpace(part[:i])
			if _, ok := kv[k]; !ok {
				order = append(order, k)
			}
			kv[k] = part[i+1:]
		}
	}
	for _, c := range cookies {
		if c.Value == "" {
			continue
		}
		if _, ok := kv[c.Name]; !ok {
			order = append(order, c.Name)
		}
		kv[c.Name] = c.Value
	}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, k+"="+kv[k])
	}
	q.streamMu.Lock()
	q.streamCookie = strings.Join(parts, "; ")
	q.streamMu.Unlock()
}

// Delete 删除自己盘里的文件（进回收站）。
func (q *Quark) Delete(ctx context.Context, cachePath string) error {
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	payload := map[string]any{
		"action_type":  2,
		"filelist":     []string{cachePath},
		"exclude_fids": []string{},
	}
	if err := q.call(ctx, http.MethodPost, quarkDriveBase, "/1/clouddrive/file/delete",
		nil, payload, &out); err != nil {
		return err
	}
	if out.Code != 0 {
		return fmt.Errorf("夸克删除失败: %s", out.Message)
	}
	return nil
}

// EmptyTrash 夸克暂不主动清空回收站。
func (q *Quark) EmptyTrash(ctx context.Context) error { return nil }

// Quota 暂不查夸克空间。
func (q *Quark) Quota(ctx context.Context) (used, total int64, err error) { return 0, 0, nil }

// ---- 可选能力 ----

// ListShare 浏览夸克分享（供收集/导入）。subPath 为分享内相对路径，空为根。
func (q *Quark) ListShare(ctx context.Context, share cache.ShareRef, subPath string) ([]cache.ShareEntry, error) {
	m := reQuarkShare.FindStringSubmatch(share.ShareURL)
	if m == nil {
		return nil, fmt.Errorf("无法解析夸克分享链接: %s", share.ShareURL)
	}
	pwdID := m[1]
	stoken, err := q.shareToken(ctx, pwdID, strings.TrimSpace(share.SharePwd))
	if err != nil {
		return nil, err
	}
	pdir := "0"
	prefix := ""
	if sp := strings.Trim(subPath, "/"); sp != "" {
		it, err := q.findInShare(ctx, pwdID, stoken, sp)
		if err != nil {
			return nil, err
		}
		pdir, prefix = it.Fid, sp+"/"
	}
	items, err := q.shareDetail(ctx, pwdID, stoken, pdir)
	if err != nil {
		return nil, err
	}
	out := make([]cache.ShareEntry, 0, len(items))
	for _, it := range items {
		out = append(out, cache.ShareEntry{
			Name:  it.FileName,
			Path:  prefix + it.FileName,
			IsDir: it.Dir,
			Size:  it.Size,
		})
	}
	return out, nil
}

// Verify 实测校验夸克 cookie 是否有效（列一次自己盘根目录）。
func (q *Quark) Verify(ctx context.Context, provider string) error {
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	qs := url.Values{"pdir_fid": {"0"}, "_page": {"1"}, "_size": {"1"}}
	if err := q.call(ctx, http.MethodGet, quarkDriveBase, "/1/clouddrive/file/sort", qs, nil, &out); err != nil {
		return err
	}
	if out.Code != 0 {
		return fmt.Errorf("夸克 cookie 无效: %s", out.Message)
	}
	return nil
}

// RefreshTokens 保活：cookie 没有续期机制，这里轻量 ping 一次保持活跃。
func (q *Quark) RefreshTokens(ctx context.Context) error {
	if q.cookie() == "" {
		return nil
	}
	return q.Verify(ctx, "quark")
}

// SaveToFolder 手动转存：把分享内某文件/目录存进自己盘的指定目录（永久留存，不受即删影响）。
func (q *Quark) SaveToFolder(ctx context.Context, share cache.ShareRef, srcPath, targetFolder string) error {
	m := reQuarkShare.FindStringSubmatch(share.ShareURL)
	if m == nil {
		return fmt.Errorf("无法解析夸克分享链接: %s", share.ShareURL)
	}
	pwdID := m[1]
	stoken, err := q.shareToken(ctx, pwdID, strings.TrimSpace(share.SharePwd))
	if err != nil {
		return err
	}
	it, err := q.findInShare(ctx, pwdID, stoken, srcPath)
	if err != nil {
		return err
	}
	toDir, err := q.ensureFolder(ctx, strings.TrimSpace(targetFolder))
	if err != nil {
		return err
	}
	return q.shareSave(ctx, pwdID, stoken, it, toDir)
}

// ensureFolder 在自己盘根目录下按名找目录，没有就建，返回其 fid。名字为空则用根目录。
func (q *Quark) ensureFolder(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "0", nil
	}
	var list struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Fid      string `json:"fid"`
				FileName string `json:"file_name"`
				Dir      bool   `json:"dir"`
			} `json:"list"`
		} `json:"data"`
	}
	qs := url.Values{"pdir_fid": {"0"}, "_page": {"1"}, "_size": {"200"}}
	if err := q.call(ctx, http.MethodGet, quarkDriveBase, "/1/clouddrive/file/sort", qs, nil, &list); err != nil {
		return "", err
	}
	for _, f := range list.Data.List {
		if f.Dir && f.FileName == name {
			return f.Fid, nil
		}
	}
	// 没找到就创建
	var made struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Fid string `json:"fid"`
		} `json:"data"`
	}
	payload := map[string]any{"pdir_fid": "0", "file_name": name, "dir_path": "", "dir_init_lock": false}
	if err := q.call(ctx, http.MethodPost, quarkDriveBase, "/1/clouddrive/file", nil, payload, &made); err != nil {
		return "", err
	}
	if made.Data.Fid == "" {
		return "", fmt.Errorf("夸克创建目录 %q 失败: %s", name, made.Message)
	}
	return made.Data.Fid, nil
}

// WalkShare 递归遍历整个分享，一次性返回所有文件条目（供批量导入资源库）。
// 深度/数量都设了上限，避免超大分享把内存和夸克接口打爆。
func (q *Quark) WalkShare(ctx context.Context, share cache.ShareRef) ([]cache.ShareEntry, error) {
	m := reQuarkShare.FindStringSubmatch(share.ShareURL)
	if m == nil {
		return nil, fmt.Errorf("无法解析夸克分享链接: %s", share.ShareURL)
	}
	pwdID := m[1]
	stoken, err := q.shareToken(ctx, pwdID, strings.TrimSpace(share.SharePwd))
	if err != nil {
		return nil, err
	}
	// 起始目录：share.FilePath 非空则从该子目录开始（「导入此目录」用）。
	startFid, prefix := "0", ""
	if sp := strings.Trim(share.FilePath, "/"); sp != "" {
		it, err := q.findInShare(ctx, pwdID, stoken, sp)
		if err != nil {
			return nil, err
		}
		startFid, prefix = it.Fid, sp+"/"
	}
	var out []cache.ShareEntry
	err = q.walkDir(ctx, pwdID, stoken, startFid, prefix, 0, &out)
	return out, err
}

const (
	quarkWalkMaxDepth = 8
	quarkWalkMaxFiles = 2000
)

// walkDir 深度优先遍历分享目录，把文件累加进 out。
func (q *Quark) walkDir(ctx context.Context, pwdID, stoken, fid, prefix string, depth int, out *[]cache.ShareEntry) error {
	if depth > quarkWalkMaxDepth || len(*out) >= quarkWalkMaxFiles {
		return nil
	}
	items, err := q.shareDetail(ctx, pwdID, stoken, fid)
	if err != nil {
		return err
	}
	for _, it := range items {
		if len(*out) >= quarkWalkMaxFiles {
			return nil
		}
		p := prefix + it.FileName
		if it.Dir {
			if err := q.walkDir(ctx, pwdID, stoken, it.Fid, p+"/", depth+1, out); err != nil {
				return err
			}
			continue
		}
		*out = append(*out, cache.ShareEntry{Name: it.FileName, Path: p, IsDir: false, Size: it.Size})
	}
	return nil
}

// findInFolderOnce 在指定目录按文件名找一次（不重试），用于判断是否已转存过。
func (q *Quark) findInFolderOnce(ctx context.Context, dirFid, name string) (string, int64, error) {
	var out struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Fid      string `json:"fid"`
				FileName string `json:"file_name"`
				Size     int64  `json:"size"`
			} `json:"list"`
		} `json:"data"`
	}
	qs := url.Values{"pdir_fid": {dirFid}, "_page": {"1"}, "_size": {"200"},
		"_sort": {"file_type:asc,updated_at:desc"}}
	if err := q.call(ctx, http.MethodGet, quarkDriveBase, "/1/clouddrive/file/sort", qs, nil, &out); err != nil {
		return "", 0, err
	}
	for _, f := range out.Data.List {
		if f.FileName == name {
			return f.Fid, f.Size, nil
		}
	}
	return "", 0, nil
}

// quarkCacheFolder 是按需转存的落地目录名（避免污染网盘根目录）。
const quarkCacheFolder = "ivideo"

// cacheFolderFid 取缓存目录的 fid（没有就建），结果记住避免重复查。
func (q *Quark) cacheFolderFid(ctx context.Context) (string, error) {
	q.cacheDirMu.Lock()
	if q.cacheDirFid != "" {
		fid := q.cacheDirFid
		q.cacheDirMu.Unlock()
		return fid, nil
	}
	q.cacheDirMu.Unlock()

	fid, err := q.ensureFolder(ctx, quarkCacheFolder)
	if err != nil {
		return "", err
	}
	q.cacheDirMu.Lock()
	q.cacheDirFid = fid
	q.cacheDirMu.Unlock()
	return fid, nil
}
