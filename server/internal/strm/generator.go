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
		rels, changed, err := g.writeOne(r)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("资源 %d(%s): %v", r.ID, r.Title, err))
			continue
		}
		for _, rel := range rels {
			want[rel] = true
		}
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

// layout 是一个资源在媒体库里应产出的文件集合。
type layout struct {
	strmRel    string // strm 相对路径
	nfoRel     string // 同目录（电影）或剧根（剧集）的 NFO 路径，空表示不写
	nfoContent string // NFO 内容
}

// planLayout 按解析出的媒体信息，规划 strm + NFO 的落盘路径（符合 Jellyfin 约定）：
//
//	电影：movies/<片名> (年份)/<片名>.strm                 —— **扁平**（电影库不认嵌套目录）
//	剧集：tv/<国家>/<剧名>/Season 0x/<剧名> S0xE0y.strm
//	动漫：anime/<国家>/<剧名>/Season 0x/<剧名> S0xE0y.strm
//
// 顶层大类（电影/剧集/动漫）由 info.Library() 决定，各对应一个 Jellyfin 库。
// 国家这一层：剧集/动漫用文件夹（可钻），电影用 NFO 的 <tag>（电影库不认嵌套）。
// 分类只承载「国家」这一个人工维度；Genre 留空，交给在线刮削填真实类型。
func (g *Generator) planLayout(r store.Resource, info MediaInfo) layout {
	country := sanitize(info.Country())

	// 电影：扁平结构 + 清理片名（剥技术标记，让 TMDB 刮得中），国家进 NFO tag。
	if info.Library() == LibMovies {
		cleanName, cleanYear := CleanMovieTitle(info.Title)
		name := sanitize(cleanName)
		if name == "" {
			name = fmt.Sprintf("resource-%d", r.ID)
		}
		folder := name
		if cleanYear > 0 {
			folder = fmt.Sprintf("%s (%d)", name, cleanYear)
		}
		dir := filepath.Join("movies", folder)
		lo := layout{strmRel: filepath.Join(dir, name+".strm")}
		if country != "" {
			lo.nfoRel = filepath.Join(dir, name+".nfo")
			lo.nfoContent = nfoTags("movie", country)
		}
		return lo
	}

	// 剧集 / 动漫：<库>/<剧名>/Season 0x/...
	// 剧名必须是库根的直接子目录 —— Jellyfin 把库根下第一层文件夹直接当成一部剧，
	// 中间再套「国家」目录会让它把国家名当成剧名去刮削（实测 anime/美国/... 被刮成
	// 「美国老爹」）。所以国家和电影一样只进 NFO 的 <tag>，不做文件夹层。
	root := string(info.Library()) // "tv" 或 "anime"
	show := sanitize(info.Title)
	if show == "" {
		show = fmt.Sprintf("resource-%d", r.ID)
	}
	showDir := filepath.Join(root, show)
	season := fmt.Sprintf("Season %02d", info.Season)
	file := fmt.Sprintf("%s S%02dE%02d.strm", show, info.Season, info.Episode)
	lo := layout{strmRel: filepath.Join(showDir, season, file)}
	if country != "" {
		lo.nfoRel = filepath.Join(showDir, "tvshow.nfo")
		lo.nfoContent = nfoTags("tvshow", country)
	}
	return lo
}

// nfoTags 生成只含标签的最小 NFO（root 为 movie / tvshow）。
// 只写 <tag>（国家等人工分类维度）；Genre/海报/演员等留给在线刮削补全。
func nfoTags(root string, tags ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString("<" + root + ">\n")
	for _, t := range tags {
		if t != "" {
			b.WriteString("  <tag>" + xmlEscape(t) + "</tag>\n")
		}
	}
	b.WriteString("</" + root + ">\n")
	return b.String()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// writeOne 为单个资源写 strm（及可选 NFO），返回本轮它拥有的所有相对路径、
// 以及 strm 是否真的写了盘。目录结构由 file_path 解析出的分类信息决定（见 planLayout）。
func (g *Generator) writeOne(r store.Resource) (rels []string, changed bool, err error) {
	info := ParsePath(r.FilePath, r.Title)
	lo := g.planLayout(r, info)

	absDir := filepath.Join(g.mediaDir, filepath.Dir(lo.strmRel))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, false, err
	}

	// hls 模式指向转码流(阿里对原画下载限速，转码流快得多)；original 指向原画直链。
	content := fmt.Sprintf("%s%s/hls/%d.m3u8", g.siteURL, g.apiPrefix, r.ID)
	if g.mode == "original" {
		content = fmt.Sprintf("%s%s/file/%d%s", g.siteURL, g.apiPrefix, r.ID, ext(r.FilePath))
	}
	changed, err = writeIfChanged(filepath.Join(g.mediaDir, lo.strmRel), content)
	if err != nil {
		return nil, false, err
	}
	rels = append(rels, lo.strmRel)

	// NFO（分类→Genre）。剧集是剧根共用一份，多集重复写但内容相同、幂等。
	if lo.nfoRel != "" {
		if _, err := writeIfChanged(filepath.Join(g.mediaDir, lo.nfoRel), lo.nfoContent); err != nil {
			return nil, false, err
		}
		rels = append(rels, lo.nfoRel)
	}
	return rels, changed, nil
}

// writeIfChanged 写文件，内容没变则跳过（不动 mtime，避免无谓触发 Jellyfin 重扫）。
func writeIfChanged(absPath, content string) (changed bool, err error) {
	if old, err := os.ReadFile(absPath); err == nil && string(old) == content {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// cleanOrphans 删除不在 want 集合里的 strm（资源已删除的残留），并清理空目录。
func (g *Generator) cleanOrphans(want map[string]bool) int {
	removed := 0
	_ = filepath.Walk(g.mediaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		e := strings.ToLower(filepath.Ext(path))
		if e != ".strm" && e != ".nfo" {
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
