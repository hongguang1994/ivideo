package backends

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ivideo/server/internal/cache"
)

// 115 转存分享（网页版 share/receive，需 cookie）。
//
// 115 开放平台没有转存接口，只能走网页版：
//  1. share/snap    列分享内文件（拿 file_id）——带 cookie 更稳
//  2. share/receive 用 cookie 把文件转存进自己盘的目标目录
//  3. 再用开放接口列目标目录，按文件名找到转存后文件的 pick_code（供播放/删除）
//
// cookie 由「设置页 115 扫码登录」存进 TokenStore(provider="115_cookie")。

const pan115Web = "https://webapi.115.com"

var reShareCode = regexp.MustCompile(`/s/([A-Za-z0-9]+)`)

// webCookie 读网页态 cookie（扫码登录存的）。
func (p *Pan115) webCookie() string {
	if p.tokens != nil {
		return p.tokens.GetToken("115_cookie")
	}
	return ""
}

// parseShareCode 从分享链接 + 提取码解析 share_code / receive_code。
func parseShareCode(shareURL, pwd string) (shareCode, receiveCode string) {
	if m := reShareCode.FindStringSubmatch(shareURL); m != nil {
		shareCode = m[1]
	}
	receiveCode = strings.TrimSpace(pwd)
	if receiveCode == "" {
		if u, err := url.Parse(shareURL); err == nil {
			receiveCode = u.Query().Get("password")
		}
	}
	return shareCode, receiveCode
}

// isPan115Share 判断是否是 115 分享链接。
func isPan115Share(shareURL string) bool {
	return strings.Contains(shareURL, "115cdn.com/s/") || strings.Contains(shareURL, "115.com/s/")
}

// keepAliveCookie 用 cookie 轻量访问一次网页接口，保持登录态活跃、尽量延长 cookie 寿命。
// 由令牌保活定时器周期调用。失败（cookie 过期/风控）只记日志，不影响 open token。
func (p *Pan115) keepAliveCookie(ctx context.Context) {
	ck := p.webCookie()
	if ck == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		pan115Web+"/files?aid=1&cid=0&offset=0&limit=1", nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", Pan115UA)
	req.Header.Set("Cookie", ck)
	resp, err := p.http.Do(req)
	if err != nil {
		slog.Warn("115 cookie 保活请求失败", "err", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

type pan115SnapItem struct {
	Name string `json:"n"`
	Fid  string `json:"fid"`
	Fc   string `json:"fc"` // "0"=目录 "1"=文件
}

// shareSnap 列分享内文件（带 cookie 更稳）。
func (p *Pan115) shareSnap(ctx context.Context, shareCode, receiveCode string) ([]pan115SnapItem, error) {
	q := url.Values{
		"share_code":   {shareCode},
		"receive_code": {receiveCode},
		"offset":       {"0"},
		"limit":        {"200"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pan115Web+"/share/snap?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", Pan115UA)
	if ck := p.webCookie(); ck != "" {
		req.Header.Set("Cookie", ck)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		State bool   `json:"state"`
		Error string `json:"error"`
		Data  struct {
			List []pan115SnapItem `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if !out.State {
		return nil, fmt.Errorf("115 浏览分享失败: %s", out.Error)
	}
	return out.Data.List, nil
}

// ListShare 实现 cache.ShareLister：浏览 115 分享内容（供收集/导入）。
// subPath 暂只支持顶层（115 snap 按 share_code 返回列表）。
func (p *Pan115) ListShare(ctx context.Context, share cache.ShareRef, subPath string) ([]cache.ShareEntry, error) {
	shareCode, receiveCode := parseShareCode(share.ShareURL, share.SharePwd)
	if shareCode == "" {
		return nil, fmt.Errorf("无法从分享链接解析 share_code: %s", share.ShareURL)
	}
	items, err := p.shareSnap(ctx, shareCode, receiveCode)
	if err != nil {
		return nil, err
	}
	out := make([]cache.ShareEntry, 0, len(items))
	for _, it := range items {
		out = append(out, cache.ShareEntry{
			Name:  it.Name,
			Path:  it.Name, // 分享内以文件名定位（转存时按名匹配）
			IsDir: it.Fc == "0",
		})
	}
	return out, nil
}

// shareReceive 用 cookie 把分享内某文件转存进目标目录 cid。
func (p *Pan115) shareReceive(ctx context.Context, shareCode, receiveCode, fileID, cid string) error {
	cookie := p.webCookie()
	if cookie == "" {
		return fmt.Errorf("未配置 115 网页 cookie，请在设置页扫码登录后再转存")
	}
	form := url.Values{
		"share_code":   {shareCode},
		"receive_code": {receiveCode},
		"file_id":      {fileID},
		"cid":          {cid},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pan115Web+"/share/receive",
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", Pan115UA)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		State bool   `json:"state"`
		Errno int    `json:"errno"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if !out.State {
		return fmt.Errorf("115 转存失败(errno=%d): %s", out.Errno, out.Error)
	}
	return nil
}

// pickCodeByName 转存完成后，用开放接口列目标目录，按文件名找 pick_code 和大小。
func (p *Pan115) pickCodeByName(ctx context.Context, cid, name string) (pickCode string, size int64, err error) {
	var out struct {
		State bool `json:"state"`
		Data  []struct {
			Fn string `json:"fn"`
			Pc string `json:"pc"`
			Fs int64  `json:"fs"`
		} `json:"data"`
	}
	q := url.Values{"cid": {cid}, "limit": {"200"}, "show_dir": {"0"}, "o": {"user_utime"}, "asc": {"0"}}
	if err := p.apiGet(ctx, "/ufile/files", q, &out); err != nil {
		return "", 0, err
	}
	for _, it := range out.Data {
		if it.Fn == name && it.Pc != "" {
			return it.Pc, it.Fs, nil
		}
	}
	return "", 0, fmt.Errorf("115 转存后未在目标目录找到文件 %q", name)
}

// receiveShareFile 完整转存一个分享内文件到自己盘根目录，返回 pick_code 和大小。
func (p *Pan115) receiveShareFile(ctx context.Context, shareURL, pwd, filePath string) (string, int64, error) {
	shareCode, receiveCode := parseShareCode(shareURL, pwd)
	if shareCode == "" {
		return "", 0, fmt.Errorf("无法从分享链接解析 share_code: %s", shareURL)
	}
	items, err := p.shareSnap(ctx, shareCode, receiveCode)
	if err != nil {
		return "", 0, err
	}
	want := filePath
	if i := strings.LastIndexByte(want, '/'); i >= 0 {
		want = want[i+1:]
	}
	var fileID, name string
	for _, it := range items {
		if it.Fc == "1" && (it.Name == want || want == "") {
			fileID, name = it.Fid, it.Name
			break
		}
	}
	if fileID == "" {
		return "", 0, fmt.Errorf("分享内未找到文件 %q", want)
	}
	const targetCID = "0" // 转存到根目录；即删会清理
	if err := p.shareReceive(ctx, shareCode, receiveCode, fileID, targetCID); err != nil {
		return "", 0, err
	}
	return p.pickCodeByName(ctx, targetCID, name)
}
