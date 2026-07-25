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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ivideo/server/internal/store"
)

// Result 是一次生成的统计。
type Result struct {
	Total     int      `json:"total"`     // 资源总数
	Written   int      `json:"written"`   // 实际写盘的 strm 数（新建或内容变了）
	Unchanged int      `json:"unchanged"` // 内容没变、跳过没动的
	Removed   int      `json:"removed"`   // 清理掉的孤儿 strm 数
	Errors    []string `json:"errors,omitempty"`
}

// Changed 表示这轮生成是否真的动了媒体库 —— 只有动了才值得让 Jellyfin 重新扫库。
func (r Result) Changed() bool { return r.Written > 0 || r.Removed > 0 }

// Generator 生成 strm 媒体库。
type Generator struct {
	store     store.Store
	mediaDir  string
	siteURL   string
	mode      string // hls(默认,流畅) / original(原画,需不限速账号)
	apiPrefix string // 接口前缀,如 /api/v1
}

// New 创建生成器。mode 为 "hls" 或 "original"；apiPrefix 如 /api/v1。
func New(st store.Store, mediaDir, siteURL, mode, apiPrefix string) *Generator {
	if mode != "original" {
		mode = "hls"
	}
	return &Generator{
		store:     st,
		mediaDir:  mediaDir,
		siteURL:   strings.TrimRight(siteURL, "/"),
		mode:      mode,
		apiPrefix: strings.TrimRight(apiPrefix, "/"),
	}
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
		rel, changed, err := g.writeOne(r)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("资源 %d(%s): %v", r.ID, r.Title, err))
			continue
		}
		want[rel] = true
		if changed {
			res.Written++
		} else {
			res.Unchanged++
		}
	}

	res.Removed = g.cleanOrphans(want)
	slog.Info("strm 生成完成", "total", res.Total,
		"written", res.Written, "unchanged", res.Unchanged, "removed", res.Removed)
	return res, nil
}

// relPathFor 按解析出的媒体信息，算出 strm 的相对落盘路径（符合 Jellyfin 约定）：
//
//	电影：movies/<分类段...>/<片名> (年份)/<片名>.strm
//	剧集：tv/<分类段...>/<剧名>/Season 0x/<剧名> S0xE0y.strm
//
// 分类段（题材/国别）原样保留成中间目录，Jellyfin 库内可按文件夹浏览。
func (g *Generator) relPathFor(r store.Resource, info MediaInfo) string {
	cats := make([]string, 0, len(info.Categories))
	for _, c := range info.Categories {
		if s := sanitize(c); s != "" {
			cats = append(cats, s)
		}
	}

	if info.Kind == KindEpisode {
		show := sanitize(info.Title)
		if show == "" {
			show = fmt.Sprintf("resource-%d", r.ID)
		}
		season := fmt.Sprintf("Season %02d", info.Season)
		file := fmt.Sprintf("%s S%02dE%02d.strm", show, info.Season, info.Episode)
		parts := append([]string{"tv"}, cats...)
		parts = append(parts, show, season, file)
		return filepath.Join(parts...)
	}

	// 电影
	name := sanitize(info.Title)
	if name == "" {
		name = fmt.Sprintf("resource-%d", r.ID)
	}
	folder := name
	if info.Year > 0 {
		folder = fmt.Sprintf("%s (%d)", name, info.Year)
	}
	parts := append([]string{"movies"}, cats...)
	parts = append(parts, folder, name+".strm")
	return filepath.Join(parts...)
}

// writeOne 为单个资源写 strm，返回相对 mediaDir 的路径，以及是否真的写了盘。
// 目录结构由 file_path 解析出的分类信息决定（见 relPathFor）。
func (g *Generator) writeOne(r store.Resource) (rel string, changed bool, err error) {
	info := ParsePath(r.FilePath, r.Title)
	relFile := g.relPathFor(r, info)

	absDir := filepath.Join(g.mediaDir, filepath.Dir(relFile))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", false, err
	}

	// hls 模式指向转码流(阿里对原画下载限速，转码流快得多)；original 指向原画直链。
	content := fmt.Sprintf("%s%s/hls/%d.m3u8", g.siteURL, g.apiPrefix, r.ID)
	if g.mode == "original" {
		content = fmt.Sprintf("%s%s/file/%d%s", g.siteURL, g.apiPrefix, r.ID, ext(r.FilePath))
	}
	absFile := filepath.Join(g.mediaDir, relFile)

	// 内容没变就不重写，避免无谓地改动 mtime 触发 Jellyfin 重扫。
	if old, err := os.ReadFile(absFile); err == nil && string(old) == content {
		return relFile, false, nil
	}
	if err := os.WriteFile(absFile, []byte(content), 0o644); err != nil {
		return "", false, err
	}
	return relFile, true, nil
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
