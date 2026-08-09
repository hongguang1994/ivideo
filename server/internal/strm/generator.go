// Package strm 负责把资源库生成为 Emby/Jellyfin 可扫描的 strm 媒体库。
//
// 生成的 strm 内容指向 ivideo 自己的网关：
//
//	http://<站点>/api/file/<资源ID>.mkv
//
// Jellyfin 播放时请求该地址 → ivideo 确保已转存进自己网盘 → 302 原画直链。
package strm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ivideo/server/internal/store"
)

// Result 是一次生成的统计。
type Result struct {
	Total        int      `json:"total"`        // 资源总数
	Written      int      `json:"written"`      // 实际写盘的 strm 数（新建或内容变了）
	Unchanged    int      `json:"unchanged"`    // 内容没变、跳过没动的
	Deduplicated int      `json:"deduplicated"` // 合并到同一作品/分集的重复来源
	Conflicts    int      `json:"conflicts"`    // 已用稳定路径隔离的同名冲突
	Removed      int      `json:"removed"`      // 清理掉的孤儿 strm 数
	Errors       []string `json:"errors,omitempty"`
}

// Generator 生成 strm 媒体库。
type Generator struct {
	store           Repository
	mediaDir        string
	siteURL         string
	mode            string // hls(默认,流畅) / original(原画,需不限速账号)
	apiPrefix       string // 接口前缀,如 /api/v1
	classifications map[int64]string
	groupStates     map[int64]store.MediaGroupState
}

// Repository is the publication-specific persistence contract.
type Repository interface {
	ListResources() ([]store.Resource, error)
	GetResourceGroupStates() (map[int64]store.MediaGroupState, error)
	RecordMediaPublication(publication store.MediaPublication) error
	RecordMediaPublicationArtifact(artifact store.MediaPublicationArtifact) error
}

// New 创建生成器。mode 为 "hls" 或 "original"；apiPrefix 如 /api/v1。
func New(st Repository, mediaDir, siteURL, mode, apiPrefix string) *Generator {
	if mode != "original" {
		mode = "hls"
	}
	return &Generator{
		store:           st,
		mediaDir:        mediaDir,
		siteURL:         strings.TrimRight(siteURL, "/"),
		mode:            mode,
		apiPrefix:       strings.TrimRight(apiPrefix, "/"),
		classifications: map[int64]string{},
		groupStates:     map[int64]store.MediaGroupState{},
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
	// 作品组状态机是正式发布的唯一依据。没有可靠作品组的资源统一进入待整理。
	if states, err := g.store.GetResourceGroupStates(); err == nil {
		g.groupStates = states
		for _, resource := range resources {
			state, ok := states[resource.ID]
			if !ok || (state.Status != "verified" && state.Status != "published") || !ValidLibrary(state.Library) {
				g.classifications[resource.ID] = string(LibReview)
				continue
			}
			g.classifications[resource.ID] = state.Library
		}
	} else {
		// 匹配状态不可读取时宁可等待人工处理，也不能把不确定内容发布到正式库。
		for _, resource := range resources {
			g.classifications[resource.ID] = string(LibReview)
		}
	}

	if err := os.MkdirAll(g.mediaDir, 0o755); err != nil {
		return res, fmt.Errorf("创建媒体目录失败: %w", err)
	}
	for _, library := range []LibraryKind{LibMovies, LibTV, LibAnime, LibVariety, LibReview} {
		if err := os.MkdirAll(filepath.Join(g.mediaDir, string(library)), 0o755); err != nil {
			return res, fmt.Errorf("创建 %s 媒体目录失败: %w", library, err)
		}
	}

	plans, summary := g.buildPlans(resources)
	res.Deduplicated = summary.deduplicated
	res.Conflicts = summary.conflicts

	// 先完成全量规划再写盘，保证相同输入永远选择同一个输出和来源。
	want := make(map[string]bool, len(resources))
	groupOutputs := map[int64][]string{}
	groupChanged := map[int64]bool{}
	groupNeedsPublication := map[int64]bool{}
	changedByPath := map[string]bool{}

	for _, plan := range plans {
		rels := planOutputPaths(plan)
		for _, rel := range rels {
			want[rel] = true
		}
		if plan.duplicateOf > 0 {
			res.Unchanged++
			continue
		}
		changed, err := g.writePlan(plan)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("资源 %d(%s): %v", plan.resource.ID, plan.resource.Title, err))
			continue
		}
		changedByPath[plan.layout.strmRel] = changed
		if changed {
			res.Written++
		} else {
			res.Unchanged++
		}
	}
	for _, plan := range plans {
		state, ok := g.groupStates[plan.resource.ID]
		if !ok || (state.Status != "verified" && state.Status != "published") {
			continue
		}
		rels := planOutputPaths(plan)
		groupOutputs[state.GroupID] = append(groupOutputs[state.GroupID], rels...)
		groupChanged[state.GroupID] = groupChanged[state.GroupID] || changedByPath[plan.layout.strmRel]
		groupNeedsPublication[state.GroupID] = groupNeedsPublication[state.GroupID] || state.Status == "verified"
	}

	res.Removed = g.cleanOrphans(want)
	res.Removed += g.cleanOrphanDirs(want)
	for groupID, outputs := range groupOutputs {
		if (!groupChanged[groupID] && !groupNeedsPublication[groupID]) || len(outputs) == 0 {
			continue
		}
		sort.Strings(outputs)
		unique := outputs[:0]
		var snapshot strings.Builder
		for _, output := range outputs {
			if len(unique) > 0 && unique[len(unique)-1] == output {
				continue
			}
			unique = append(unique, output)
			snapshot.WriteString(output)
			snapshot.WriteByte('\n')
			if content, err := os.ReadFile(filepath.Join(g.mediaDir, output)); err == nil {
				snapshot.Write(content)
				snapshot.WriteByte('\n')
			}
		}
		digest := sha256.Sum256([]byte(snapshot.String()))
		_ = g.store.RecordMediaPublication(store.MediaPublication{
			GroupID: groupID, Status: "published", OutputPath: filepath.Dir(unique[0]), MetadataHash: hex.EncodeToString(digest[:]),
		})
		for _, output := range unique {
			contentHash := ""
			if content, err := os.ReadFile(filepath.Join(g.mediaDir, output)); err == nil {
				artifactDigest := sha256.Sum256(content)
				contentHash = hex.EncodeToString(artifactDigest[:])
			}
			_ = g.store.RecordMediaPublicationArtifact(store.MediaPublicationArtifact{
				GroupID: groupID, ArtifactType: artifactType(output), Path: filepath.Join(g.mediaDir, output),
				ContentHash: contentHash, Status: "ready",
			})
		}
	}
	slog.Info("strm 生成完成", "total", res.Total,
		"written", res.Written, "unchanged", res.Unchanged, "deduplicated", res.Deduplicated,
		"conflicts", res.Conflicts, "removed", res.Removed)
	return res, nil
}

// cleanOrphanDirs 清理分类调整后遗留的旧作品目录。
// 媒体目录完全由 STRM 生成器维护；只有没有任何目标 .strm 的目录才会被删除，
// 防止旧的 NFO/海报继续被 Jellyfin 当成幽灵作品展示。
func (g *Generator) cleanOrphanDirs(want map[string]bool) int {
	keep := map[string]bool{".": true}
	for rel := range want {
		if strings.ToLower(filepath.Ext(rel)) != ".strm" {
			continue
		}
		for dir := filepath.Dir(rel); ; dir = filepath.Dir(dir) {
			keep[dir] = true
			if dir == "." {
				break
			}
		}
	}
	var dirs []string
	for _, library := range []LibraryKind{LibMovies, LibTV, LibAnime, LibVariety, LibReview} {
		root := filepath.Join(g.mediaDir, string(library))
		_ = filepath.Walk(root, func(abs string, info os.FileInfo, err error) error {
			if err == nil && info != nil && info.IsDir() {
				dirs = append(dirs, abs)
			}
			return nil
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	removed := 0
	for _, abs := range dirs {
		rel, err := filepath.Rel(g.mediaDir, abs)
		if err != nil || rel == "." || keep[rel] || isLibraryRoot(g.mediaDir, abs) {
			continue
		}
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			continue
		}
		if err := os.RemoveAll(abs); err == nil {
			removed++
		}
	}
	return removed
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
	country := Sanitize(info.Country())

	// 电影：扁平结构 + 清理片名（剥技术标记，让 TMDB 刮得中），国家进 NFO tag。
	if info.Library() == LibMovies || info.Library() == LibReview {
		cleanName, cleanYear := CleanMovieTitle(info.Title)
		name := Sanitize(cleanName)
		// 清理后为空（如 "03.mp4" 这种纯数字分集名）：用「父目录名 + 原标题」，
		// 比 resource-<id> 可读得多，Jellyfin 里也能看出是哪部片的第几集。
		if name == "" {
			base := Sanitize(info.Title)
			if parent := parentDirName(r.FilePath); parent != "" {
				if base != "" {
					name = Sanitize(parent + " " + base)
				} else {
					name = Sanitize(parent)
				}
			} else {
				name = base
			}
		}
		if name == "" {
			name = fmt.Sprintf("resource-%d", r.ID)
		}
		folder := name
		if cleanYear > 0 {
			folder = fmt.Sprintf("%s (%d)", name, cleanYear)
		}
		root := "movies"
		if info.Library() == LibReview {
			root = "review"
			// 待整理项按资源编号隔离，避免同名资源共用旧 NFO。
			folder = fmt.Sprintf("resource-%d - %s", r.ID, name)
		}
		dir := filepath.Join(root, folder)
		lo := layout{strmRel: filepath.Join(dir, name+".strm")}
		lo.nfoRel = filepath.Join(dir, name+".nfo")
		lo.nfoContent = nfoFallback("movie", name, country)
		return lo
	}

	// 剧集 / 动漫 / 综艺：<库>/<剧名>/Season 0x/...
	// 剧名必须是库根的直接子目录 —— Jellyfin 把库根下第一层文件夹直接当成一部剧，
	// 中间再套「国家」目录会让它把国家名当成剧名去刮削（实测 anime/美国/... 被刮成
	// 「美国老爹」）。所以国家和电影一样只进 NFO 的 <tag>，不做文件夹层。
	root := string(info.Library()) // "tv"、"anime" 或 "variety"
	show := Sanitize(info.Title)
	if show == "" {
		show = fmt.Sprintf("resource-%d", r.ID)
	}
	showDir := filepath.Join(root, show)
	season := fmt.Sprintf("Season %02d", info.Season)
	file := fmt.Sprintf("%s S%02dE%02d.strm", show, info.Season, info.Episode)
	lo := layout{strmRel: filepath.Join(showDir, season, file)}
	lo.nfoRel = filepath.Join(showDir, "tvshow.nfo")
	lo.nfoContent = nfoFallback("tvshow", show, country)
	return lo
}

// nfoFallback 为尚未写入完整资料的项目提供确定性元数据。即使 Jellyfin 扫描
// 发生在后端刮削之前，也只能显示这个标题，不能再联网把数字分集猜成其他电影。
func nfoFallback(root, title string, tags ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString("<" + root + ">\n")
	b.WriteString("  <title>" + xmlEscape(title) + "</title>\n")
	for _, t := range tags {
		if t != "" {
			b.WriteString("  <tag>" + xmlEscape(t) + "</tag>\n")
		}
	}
	b.WriteString("  <lockdata>true</lockdata>\n")
	b.WriteString("</" + root + ">\n")
	return b.String()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// writePlan 写入已经完成冲突消解的发布计划。
func (g *Generator) writePlan(plan resourcePlan) (bool, error) {
	absDir := filepath.Join(g.mediaDir, filepath.Dir(plan.layout.strmRel))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return false, err
	}
	changed, err := writeIfChanged(filepath.Join(g.mediaDir, plan.layout.strmRel), plan.content)
	if err != nil {
		return false, err
	}

	// 只在 NFO 不存在时写最小分类信息。完整 NFO 由 metadata 刮削服务维护，
	// 后续重建 STRM 不得覆盖已经刮削好的资料。
	if plan.layout.nfoRel != "" {
		nfoPath := filepath.Join(g.mediaDir, plan.layout.nfoRel)
		if _, err := os.Stat(nfoPath); os.IsNotExist(err) {
			if _, err := writeIfChanged(nfoPath, plan.layout.nfoContent); err != nil {
				return false, err
			}
		} else if err != nil {
			return false, err
		}
	}
	return changed, nil
}

func planOutputPaths(plan resourcePlan) []string {
	paths := []string{plan.layout.strmRel}
	if plan.layout.nfoRel != "" {
		paths = append(paths, plan.layout.nfoRel)
	}
	return paths
}

func artifactType(path string) string {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if extension == "" {
		return "artifact"
	}
	return extension
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

// cleanOrphans 只删除不在 want 集合里的 strm。NFO 和图片由 metadata 服务维护，
// 不能因为 STRM 规划里没有列出就删除。
func (g *Generator) cleanOrphans(want map[string]bool) int {
	removed := 0
	_ = filepath.Walk(g.mediaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		e := strings.ToLower(filepath.Ext(path))
		if e != ".strm" {
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
	g.removeOrphanShowDirs()
	g.removeEmptyDirs()
	return removed
}

// removeOrphanShowDirs 清理已迁移布局遗留的剧目录。媒体目录完全由生成器维护；
// 一个作品目录不再含 STRM 时，其中的旧 NFO 和图片也必须移除，否则 Jellyfin
// 会继续把它显示成重复作品。
func (g *Generator) removeOrphanShowDirs() {
	for _, library := range []LibraryKind{LibMovies, LibTV, LibAnime, LibVariety, LibReview} {
		root := filepath.Join(g.mediaDir, string(library))
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(root, entry.Name())
			hasSTRM := false
			_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".strm") {
					hasSTRM = true
					return filepath.SkipAll
				}
				return nil
			})
			if !hasSTRM {
				_ = os.RemoveAll(dir)
			}
		}
	}
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
		if isLibraryRoot(g.mediaDir, dirs[i]) {
			continue
		}
		if entries, err := os.ReadDir(dirs[i]); err == nil && len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}
}

func isLibraryRoot(mediaDir, dir string) bool {
	rel, err := filepath.Rel(mediaDir, dir)
	if err != nil || strings.Contains(rel, string(filepath.Separator)) {
		return false
	}
	switch LibraryKind(rel) {
	case LibMovies, LibTV, LibAnime, LibVariety, LibReview:
		return true
	default:
		return false
	}
}

// sanitize 去掉文件名里的非法字符。
// Sanitize 把网盘文件名转换为 Jellyfin 媒体目录可安全使用的名称。
func Sanitize(s string) string {
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

// parentDirName 取 file_path 里文件所在目录的名字（用于给无意义文件名兜底命名）。
func parentDirName(filePath string) string {
	segs := splitClean(filePath)
	if len(segs) < 2 {
		return ""
	}
	return segs[len(segs)-2]
}
