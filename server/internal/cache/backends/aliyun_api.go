package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 阿里云盘接口地址。
const (
	aliWebTokenURL   = "https://auth.alipan.com/v2/account/token"             // web refresh → access token
	aliShareTokenURL = "https://api.alipan.com/v2/share_link/get_share_token" // 分享 token
	aliShareListURL  = "https://api.alipan.com/adrive/v3/file/list"           // 列分享内文件
	aliCopyURL       = "https://api.alipan.com/adrive/v2/file/copy"           // 转存(分享→自己盘)
	aliOpenBase      = "https://openapi.alipan.com"                           // 开放接口
)

// doJSON 发一个 JSON 请求并把响应 data 解到 out；非 2xx 返回带响应体的错误。
func (a *Aliyun) doJSON(ctx context.Context, method, url string, headers map[string]string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("阿里接口 %s 返回 %d: %s", url, resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// refreshWeb 用 web refresh token 换 access token，并记录自己盘 drive_id。
func (a *Aliyun) refreshWeb(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.webTok != "" && time.Now().Before(a.webExp) {
		tok := a.webTok
		a.mu.Unlock()
		return tok, nil
	}
	a.mu.Unlock()

	var out struct {
		AccessToken     string `json:"access_token"`
		RefreshToken    string `json:"refresh_token"`
		ExpiresIn       int    `json:"expires_in"`
		DefaultDriveID  string `json:"default_drive_id"`
		ResourceDriveID string `json:"resource_drive_id"`
	}
	body := map[string]string{"grant_type": "refresh_token", "refresh_token": a.webRT}
	if err := a.doJSON(ctx, http.MethodPost, aliWebTokenURL, nil, body, &out); err != nil {
		return "", fmt.Errorf("刷新 web token 失败: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("刷新 web token 未返回 access_token")
	}

	a.mu.Lock()
	a.webTok = out.AccessToken
	a.webExp = time.Now().Add(time.Duration(max(out.ExpiresIn-60, 60)) * time.Second)
	if out.RefreshToken != "" {
		a.webRT = out.RefreshToken // 阿里 refresh token 会轮换（仅内存保存，见 aliyun.go 说明）
	}
	if a.driveID == "" {
		if out.ResourceDriveID != "" {
			a.driveID = out.ResourceDriveID
		} else {
			a.driveID = out.DefaultDriveID
		}
	}
	tok := a.webTok
	a.mu.Unlock()
	return tok, nil
}

// refreshOpen 用开放接口 refresh token 换 access token。
func (a *Aliyun) refreshOpen(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.openTok != "" && time.Now().Before(a.openExp) {
		tok := a.openTok
		a.mu.Unlock()
		return tok, nil
	}
	a.mu.Unlock()

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	body := map[string]string{
		"client_id":     a.clientID,
		"client_secret": a.clientSecret,
		"grant_type":    "refresh_token",
		"refresh_token": a.openRT,
	}
	if err := a.doJSON(ctx, http.MethodPost, a.openTokenURL, nil, body, &out); err != nil {
		return "", fmt.Errorf("刷新 open token 失败: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("刷新 open token 未返回 access_token")
	}

	a.mu.Lock()
	a.openTok = out.AccessToken
	a.openExp = time.Now().Add(time.Duration(max(out.ExpiresIn-60, 60)) * time.Second)
	if out.RefreshToken != "" {
		a.openRT = out.RefreshToken
	}
	tok := a.openTok
	a.mu.Unlock()
	return tok, nil
}

// shareToken 取某分享的访问 token（后续列表/转存要带 x-share-token）。
func (a *Aliyun) shareToken(ctx context.Context, shareID, sharePwd string) (string, error) {
	var out struct {
		ShareToken string `json:"share_token"`
	}
	body := map[string]string{"share_id": shareID, "share_pwd": sharePwd}
	if err := a.doJSON(ctx, http.MethodPost, aliShareTokenURL, nil, body, &out); err != nil {
		return "", err
	}
	if out.ShareToken == "" {
		return "", fmt.Errorf("未取到 share_token")
	}
	return out.ShareToken, nil
}

// shareItem 是分享目录项。
type shareItem struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Type   string `json:"type"` // file / folder
	Size   int64  `json:"size"`
}

// listShare 列出分享内某目录，自动翻页。
func (a *Aliyun) listShare(ctx context.Context, shareID, shareToken, parentID string) ([]shareItem, error) {
	if parentID == "" {
		parentID = "root"
	}
	headers := map[string]string{"x-share-token": shareToken}
	var items []shareItem
	marker := ""
	for {
		body := map[string]any{
			"share_id":        shareID,
			"parent_file_id":  parentID,
			"limit":           200,
			"order_by":        "name",
			"order_direction": "ASC",
		}
		if marker != "" {
			body["marker"] = marker
		}
		var out struct {
			Items      []shareItem `json:"items"`
			NextMarker string      `json:"next_marker"`
		}
		if err := a.doJSON(ctx, http.MethodPost, aliShareListURL, headers, body, &out); err != nil {
			return nil, err
		}
		items = append(items, out.Items...)
		if out.NextMarker == "" {
			break
		}
		marker = out.NextMarker
	}
	return items, nil
}

// resolveFileID 把分享内的相对路径解析成 file_id，并返回大小。
// path 形如 "/电影/xxx.mp4"；为空则报错（必须指到具体文件）。
func (a *Aliyun) resolveFileID(ctx context.Context, shareID, shareToken, path string) (string, int64, error) {
	segs := splitPath(path)
	if len(segs) == 0 {
		return "", 0, fmt.Errorf("分享内文件路径为空")
	}
	parent := "root"
	for i, seg := range segs {
		items, err := a.listShare(ctx, shareID, shareToken, parent)
		if err != nil {
			return "", 0, err
		}
		var hit *shareItem
		for j := range items {
			if items[j].Name == seg {
				hit = &items[j]
				break
			}
		}
		if hit == nil {
			return "", 0, fmt.Errorf("分享内找不到: %s", seg)
		}
		if i == len(segs)-1 {
			return hit.FileID, hit.Size, nil
		}
		parent = hit.FileID
	}
	return "", 0, fmt.Errorf("路径解析失败")
}

// copyFromShare 把分享内文件转存到自己盘临时目录，返回新 file_id。
func (a *Aliyun) copyFromShare(ctx context.Context, webTok, shareID, shareToken, fileID string) (string, error) {
	headers := map[string]string{
		"Authorization": "Bearer " + webTok,
		"x-share-token": shareToken,
	}
	body := map[string]any{
		"file_id":           fileID,
		"share_id":          shareID,
		"auto_rename":       true,
		"to_parent_file_id": a.tempFolderID,
		"to_drive_id":       a.driveID,
	}
	var out struct {
		FileID      string `json:"file_id"`
		AsyncTaskID string `json:"async_task_id"`
	}
	if err := a.doJSON(ctx, http.MethodPost, aliCopyURL, headers, body, &out); err != nil {
		return "", err
	}
	if out.FileID != "" {
		return out.FileID, nil // 秒传/同步完成
	}
	// 非秒传会返回异步任务，需轮询 async_task；首版暂不处理。
	return "", fmt.Errorf("转存返回异步任务(async_task_id=%s)，该文件非秒传，首版暂未支持轮询", out.AsyncTaskID)
}

// openDownloadURL 用开放接口取自己盘文件的可播直链。
func (a *Aliyun) openDownloadURL(ctx context.Context, openTok, fileID string) (string, error) {
	headers := map[string]string{"Authorization": "Bearer " + openTok}
	body := map[string]any{"drive_id": a.driveID, "file_id": fileID, "expire_sec": 14400}
	var out struct {
		URL string `json:"url"`
	}
	if err := a.doJSON(ctx, http.MethodPost, aliOpenBase+"/adrive/v1.0/openFile/getDownloadUrl", headers, body, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("未取到下载直链")
	}
	return out.URL, nil
}

// openDelete 用开放接口永久删除自己盘文件（跳过回收站）。
func (a *Aliyun) openDelete(ctx context.Context, openTok, fileID string) error {
	headers := map[string]string{"Authorization": "Bearer " + openTok}
	body := map[string]any{"drive_id": a.driveID, "file_id": fileID}
	return a.doJSON(ctx, http.MethodPost, aliOpenBase+"/adrive/v1.0/openFile/delete", headers, body, nil)
}

// openSpaceInfo 查询自己盘空间用量。
func (a *Aliyun) openSpaceInfo(ctx context.Context, openTok string) (used, total int64, err error) {
	headers := map[string]string{"Authorization": "Bearer " + openTok}
	var out struct {
		PersonalSpaceInfo struct {
			UsedSize  int64 `json:"used_size"`
			TotalSize int64 `json:"total_size"`
		} `json:"personal_space_info"`
	}
	if err = a.doJSON(ctx, http.MethodPost, aliOpenBase+"/adrive/v1.0/openFile/getSpaceInfo", headers, map[string]any{}, &out); err != nil {
		return 0, 0, err
	}
	return out.PersonalSpaceInfo.UsedSize, out.PersonalSpaceInfo.TotalSize, nil
}

// splitPath 把 "/a/b/c" 切成 [a b c]。
func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
