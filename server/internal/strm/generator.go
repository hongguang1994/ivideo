// Package strm 负责把资源库生成为 Emby/Jellyfin 可扫描的 strm 媒体库。
//
// 生成的 strm 内容指向 ivideo 自己的网关：
//
//	http://<站点>/api/file/<资源ID>.mkv
//
// Jellyfin 播放时请求该地址 → ivideo 确保已转存进自己网盘 → 302 原画直链。
package strm

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"ivideo/server/internal/store"
)

// Result 是一次生成的统计。
type Result struct {
	Total   int      `json:"total"`   // 资源总数
	Written int      `json:"written"` // 新建/更新的 strm 数
	Removed int      `json:"removed"` // 清理掉的孤儿 strm 数
	Errors  []string `json:"errors,omitempty"`
}

// Generator 生成 strm 媒体库。
type Generator struct {
	store    *store.Store
	mediaDir string
	siteURL  string
	mode     string // hls(默认,流畅) / original(原画,需不限速账号)
}

// New 创建生成器。mode 为 "hls" 或 "original"。
func New(st *store.Store, mediaDir, siteURL, mode string) *Generator {
	if mode != "original" {
		mode = "hls"
	}
	return &Generator{store: st, mediaDir: mediaDir, siteURL: strings.TrimRight(siteURL, "/"), mode: mode}
}

// Generate 全量重建 strm 媒体库：为每个资源写一个 strm，并清理孤儿文件。
func (g *Generator) Generate() (Result, error) {
	var res Result

	resources, err := g.store.ListResources()
	if err != nil {
		return res, err
	}
	res.Total = len(resources)

	if err := os.MkdirAll(g.mediaDir, 0o755); err != nil {
		return res, fmt.Errorf("创建媒体目录失败: %w", err)
	}

	// 本轮应存在的 strm 文件集合，用于之后清理孤儿。
	want := make(map[string]bool, len(resources))

	for _, r := range resources {
		rel, err := g.writeOne(r)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("资源 %d(%s): %v", r.ID, r.Title, err))
			continue
		}
		want[rel] = true
		res.Written++
	}

	res.Removed = g.cleanOrphans(want)
	log.Printf("strm 生成完成: 资源=%d 写入=%d 清理=%d", res.Total, res.Written, res.Removed)
	return res, nil
}

// writeOne 为单个资源写 strm，返回相对 mediaDir 的路径。
// 结构：<媒体目录>/<标题>/<标题>.strm —— 符合 Jellyfin 电影目录约定。
func (g *Generator) writeOne(r store.Resource) (string, error) {
	name := sanitize(r.Title)
	if name == "" {
		name = fmt.Sprintf("resource-%d", r.ID)
	}
	relDir := name
	relFile := filepath.Join(relDir, name+".strm")

	absDir := filepath.Join(g.mediaDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", err
	}

	// hls 模式指向转码流(阿里对原画下载限速，转码流快得多)；original 指向原画直链。
	content := fmt.Sprintf("%s/api/hls/%d.m3u8", g.siteURL, r.ID)
	if g.mode == "original" {
		content = fmt.Sprintf("%s/api/file/%d%s", g.siteURL, r.ID, ext(r.FilePath))
	}
	absFile := filepath.Join(g.mediaDir, relFile)

	// 内容没变就不重写，避免无谓地改动 mtime 触发 Jellyfin 重扫。
	if old, err := os.ReadFile(absFile); err == nil && string(old) == content {
		return relFile, nil
	}
	if err := os.WriteFile(absFile, []byte(content), 0o644); err != nil {
		return "", err
	}
	return relFile, nil
}

// cleanOrphans 删除不在 want 集合里的 strm（资源已删除的残留），并清理空目录。
func (g *Generator) cleanOrphans(want map[string]bool) int {
	removed := 0
	_ = filepath.Walk(g.mediaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".strm") {
			return nil
		}
		rel, rerr := filepath.Rel(g.mediaDir, path)
		if rerr != nil || want[rel] {
			return nil
		}
		if os.Remove(path) == nil {
			removed++
		}
		return nil
	})
	g.removeEmptyDirs()
	return removed
}

// removeEmptyDirs 自底向上清理空目录。
func (g *Generator) removeEmptyDirs() {
	var dirs []string
	_ = filepath.Walk(g.mediaDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && path != g.mediaDir {
			dirs = append(dirs, path)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		if entries, err := os.ReadDir(dirs[i]); err == nil && len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}
}

// sanitize 去掉文件名里的非法字符。
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	bad := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", "\n", "\r", "\t"}
	for _, b := range bad {
		s = strings.ReplaceAll(s, b, " ")
	}
	s = strings.Join(strings.Fields(s), " ") // 折叠多余空格
	if len(s) > 120 {
		s = s[:120]
	}
	return strings.TrimSpace(s)
}

// ext 取分享内文件的扩展名，缺省 .mkv（仅作为给播放器的格式提示）。
func ext(filePath string) string {
	e := strings.ToLower(filepath.Ext(filePath))
	switch e {
	case ".mp4", ".mkv", ".ts", ".flv", ".avi", ".mov", ".m4v", ".webm":
		return e
	}
	return ".mkv"
}
