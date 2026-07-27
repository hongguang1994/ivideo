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
}

// NewQuark 创建夸克适配器。cookie 从 TokenStore(provider="quark_cookie") 读，
// 由「设置页夸克扫码登录」写入。
func NewQuark(cfg config.Config, tokens TokenStore) *Quark {
	return &Quark{
		http:   &http.Client{Timeout: 30 * time.Second},
		tokens: tokens,
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
	if err := q.shareSave(ctx, pwdID, stoken, it, "0"); err != nil {
		return cache.TransferResult{}, err
	}
	// 转存是异步任务，稍后按文件名在自己盘里找到 fid。
	fid, size, err := q.findOwnFileByName(ctx, it.FileName)
	if err != nil {
		return cache.TransferResult{}, err
	}
	return cache.TransferResult{CachePath: fid, Size: size}, nil
}

// findOwnFileByName 在自己盘根目录按文件名找 fid（转存后定位用，带重试等异步任务落地）。
func (q *Quark) findOwnFileByName(ctx context.Context, name string) (string, int64, error) {
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
		qs := url.Values{"pdir_fid": {"0"}, "_page": {"1"}, "_size": {"200"},
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
	return "", fmt.Errorf("夸克播放暂不可用（直链防盗链 412 未攻克）；文件已转存到你的夸克盘，可用夸克 App 观看")
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
