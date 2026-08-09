package jellyfin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

type virtualFolder struct {
	Name           string         `json:"Name"`
	Locations      []string       `json:"Locations"`
	ItemID         string         `json:"ItemId"`
	LibraryOptions map[string]any `json:"LibraryOptions"`
}

type libraryDefinition struct {
	name           string
	collectionType string
	path           string
}

// EnsureLibraries 确保 ivideo 的 STRM 目录已经注册为 Jellyfin 媒体库。
// 方法是幂等的，服务重启时可安全重复调用。
func (c *Client) EnsureLibraries(mediaDir string) error {
	var folders []virtualFolder
	if err := c.getJSON("/Library/VirtualFolders", &folders); err != nil {
		return fmt.Errorf("读取 Jellyfin 媒体库: %w", err)
	}

	definitions := []libraryDefinition{
		{name: "电影", collectionType: "movies", path: filepath.ToSlash(filepath.Join(mediaDir, "movies"))},
		{name: "剧集", collectionType: "tvshows", path: filepath.ToSlash(filepath.Join(mediaDir, "tv"))},
		{name: "动漫", collectionType: "tvshows", path: filepath.ToSlash(filepath.Join(mediaDir, "anime"))},
		{name: "综艺", collectionType: "tvshows", path: filepath.ToSlash(filepath.Join(mediaDir, "variety"))},
		{name: "待整理", collectionType: "movies", path: filepath.ToSlash(filepath.Join(mediaDir, "review"))},
	}

	for _, definition := range definitions {
		folderExists, pathExists := false, false
		for _, folder := range folders {
			if folder.Name == definition.name {
				folderExists = true
			}
			for _, location := range folder.Locations {
				if filepath.Clean(location) == filepath.Clean(definition.path) {
					pathExists = true
				}
			}
		}
		if pathExists {
			continue
		}
		if folderExists {
			if err := c.addMediaPath(definition.name, definition.path); err != nil {
				return err
			}
			continue
		}
		if err := c.addVirtualFolder(definition); err != nil {
			return err
		}
	}
	return c.enforceLocalMetadata(mediaDir)
}

// enforceLocalMetadata 把 ivideo 创建的媒体库固定为“本地元数据模式”。作品识别、
// 海报和分集资料由后端写入 NFO/图片；Jellyfin 只负责索引与播放，不能再根据
// 数字文件名调用在线提供方猜测成另一部作品。
func (c *Client) enforceLocalMetadata(mediaDir string) error {
	var folders []virtualFolder
	if err := c.getJSON("/Library/VirtualFolders", &folders); err != nil {
		return fmt.Errorf("重新读取 Jellyfin 媒体库: %w", err)
	}
	typesByName := map[string][]string{
		"电影":  {"Movie"},
		"剧集":  {"Series", "Season", "Episode"},
		"动漫":  {"Series", "Season", "Episode"},
		"综艺":  {"Series", "Season", "Episode"},
		"待整理": {"Movie"},
	}
	root := filepath.Clean(mediaDir)
	for _, folder := range folders {
		types, managed := typesByName[folder.Name]
		if !managed || folder.ItemID == "" || !hasManagedPath(folder.Locations, root) {
			continue
		}
		options := folder.LibraryOptions
		if options == nil {
			options = map[string]any{}
		}
		options["EnableInternetProviders"] = false
		options["AutomaticRefreshIntervalDays"] = 0
		options["DisabledLocalMetadataReaders"] = []string{}
		typeOptions := make([]map[string]any, 0, len(types))
		for _, mediaType := range types {
			typeOptions = append(typeOptions, map[string]any{
				"Type":                 mediaType,
				"MetadataFetchers":     []string{},
				"MetadataFetcherOrder": []string{},
				"ImageFetchers":        []string{},
				"ImageFetcherOrder":    []string{},
				"ImageOptions":         []any{},
			})
		}
		options["TypeOptions"] = typeOptions
		if err := c.postLibraryJSON("/Library/VirtualFolders/LibraryOptions", map[string]any{
			"Id": folder.ItemID, "LibraryOptions": options,
		}); err != nil {
			return fmt.Errorf("锁定 Jellyfin 媒体库 %s 的本地元数据: %w", folder.Name, err)
		}
	}
	return nil
}

func hasManagedPath(locations []string, mediaDir string) bool {
	for _, location := range locations {
		path := filepath.Clean(location)
		if path == mediaDir || strings.HasPrefix(path, mediaDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (c *Client) addVirtualFolder(definition libraryDefinition) error {
	query := url.Values{}
	query.Set("name", definition.name)
	query.Set("collectionType", definition.collectionType)
	query.Add("paths", definition.path)
	query.Set("refreshLibrary", "false")
	return c.postLibraryJSON("/Library/VirtualFolders?"+query.Encode(), map[string]any{})
}

func (c *Client) addMediaPath(name, path string) error {
	return c.postLibraryJSON("/Library/VirtualFolders/Paths?refreshLibrary=false", map[string]string{
		"Name": name,
		"Path": path,
	})
}

func (c *Client) postLibraryJSON(path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setTokenHeaders(req, c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("创建 Jellyfin 媒体库失败: %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
