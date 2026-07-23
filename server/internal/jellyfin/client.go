// Package jellyfin 是 Jellyfin 媒体服务器 REST API 的最小客户端，
// 负责列出片库条目、取海报图、取播放流地址。鉴权用后台生成的 API Key。
package jellyfin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client 封装对 Jellyfin 的调用。
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New 创建 Jellyfin 客户端。
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Item 对应 Jellyfin 条目的部分字段。
type Item struct {
	ID             string            `json:"Id"`
	Name           string            `json:"Name"`
	Type           string            `json:"Type"`
	Overview       string            `json:"Overview"`
	ProductionYear int               `json:"ProductionYear"`
	ImageTags      map[string]string `json:"ImageTags"`
}

// itemsResp 是 /Items 的响应结构。
type itemsResp struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

// Items 列出片库中的条目。itemTypes 例如 "Movie" 或 "Movie,Series"。
func (c *Client) Items(itemTypes string) ([]Item, error) {
	if itemTypes == "" {
		itemTypes = "Movie"
	}
	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("IncludeItemTypes", itemTypes)
	q.Set("Fields", "Overview,ProductionYear")
	q.Set("SortBy", "SortName")
	q.Set("SortOrder", "Ascending")

	var out itemsResp
	if err := c.getJSON("/Items?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// ImageURL 返回某条目主海报的内部访问地址（带 api_key，供后端代理拉取）。
func (c *Client) ImageURL(id string) string {
	return fmt.Sprintf("%s/Items/%s/Images/Primary?api_key=%s", c.baseURL, id, url.QueryEscape(c.apiKey))
}

// StreamURL 返回某条目的直连播放地址（供后端代理转发）。
func (c *Client) StreamURL(id string) string {
	return fmt.Sprintf("%s/Videos/%s/stream?static=true&api_key=%s", c.baseURL, id, url.QueryEscape(c.apiKey))
}

// PlayingItem 是某个会话里正在播放/暂停的条目。
type PlayingItem struct {
	ItemID   string // Jellyfin 条目 ID
	Name     string
	Path     string // 条目文件路径（通常是 .strm 文件），可能为空
	MediaURL string // 媒体源地址（strm 里那条 URL），可能为空
	Paused   bool   // 是否暂停（暂停也算“在会话中”，不该被清理）
}

// NowPlaying 拉取当前所有会话里正在播放/暂停的条目。
// 需要有效的 API Key（管理员令牌），否则 /Sessions 返回 401。
func (c *Client) NowPlaying() ([]PlayingItem, error) {
	var sessions []struct {
		NowPlayingItem *struct {
			ID           string `json:"Id"`
			Name         string `json:"Name"`
			Path         string `json:"Path"`
			MediaSources []struct {
				Path string `json:"Path"`
			} `json:"MediaSources"`
		} `json:"NowPlayingItem"`
		PlayState struct {
			IsPaused bool `json:"IsPaused"`
		} `json:"PlayState"`
	}
	if err := c.getJSON("/Sessions", &sessions); err != nil {
		return nil, err
	}
	var out []PlayingItem
	for _, s := range sessions {
		if s.NowPlayingItem == nil {
			continue
		}
		pi := PlayingItem{
			ItemID: s.NowPlayingItem.ID,
			Name:   s.NowPlayingItem.Name,
			Path:   s.NowPlayingItem.Path,
			Paused: s.PlayState.IsPaused,
		}
		if len(s.NowPlayingItem.MediaSources) > 0 {
			pi.MediaURL = s.NowPlayingItem.MediaSources[0].Path
		}
		out = append(out, pi)
	}
	return out, nil
}

// getJSON 带 API Key 发起 GET 并解 JSON。
func (c *Client) getJSON(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jellyfin: 接口返回 %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
