package backends

// 针对真实阿里云盘「分享转存」链路的集成测试。默认跳过。
//
// ── ① ListShare(只读、匿名)──────────────────────────────
// 只需分享链接,不需要任何 token,不会改动任何数据。先用它验证
// share_token / 列目录 / 路径解析是否正常:
//
//	ALIYUN_TEST_SHARE_URL=https://www.alipan.com/s/xxxx \
//	ALIYUN_TEST_SHARE_PWD=提取码(没有就留空) \
//	go test ./internal/cache/backends/ -run TestAliyunListShare -v
//
// 可选 ALIYUN_TEST_SUBPATH 指定列哪个子目录(默认列根)。
//
// ── ② SaveToFolder(真转存)────────────────────────────────
// 把分享内某文件「秒转存」进你自己盘,用来验证 openlist 做不到的那一步。
// ⚠️ 它会:写入你的网盘、并轮换你的 web refresh_token(阿里每次刷新换新值,
// 旧值作废 —— 会导致 ivideo 库里那个 token 失效!)。故必须显式开启:
//
//	ALIYUN_TEST_DO_TRANSFER=1 \
//	ALIYUN_TEST_REFRESH_TOKEN=<你的网页版 refresh_token> \
//	ALIYUN_TEST_SHARE_URL=https://www.alipan.com/s/xxxx \
//	ALIYUN_TEST_SHARE_PWD=提取码 \
//	ALIYUN_TEST_FILE_PATH=/分享内/要转的/文件.mp4 \
//	ALIYUN_TEST_TARGET=ivideo \
//	go test ./internal/cache/backends/ -run TestAliyunSaveToFolder -v

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
)

// 阿里各接口的默认域名(与 configs/conf.example.yaml 一致)。
const (
	aliAPIBase   = "https://api.alipan.com"
	aliAuthBase  = "https://auth.alipan.com"
	aliOpenBase  = "https://openapi.alipan.com"
	aliUserBase  = "https://user.alipan.com"
	aliBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// captureTokenStore 提供 web refresh_token,并捕获轮换后的新值。
// 阿里每次用 refresh_token 刷新都会返回一个新的、旧的立即作废,
// 适配器会调 SaveToken 回写 —— 这里把新值记下来,测试结束时提示用户。
type captureTokenStore struct {
	web     string
	rotated string
}

func (c *captureTokenStore) GetToken(provider string) string {
	if provider == "aliyun" {
		return c.web
	}
	return ""
}
func (c *captureTokenStore) GetTokenExtra(string) string { return "" }
func (c *captureTokenStore) SaveToken(provider, token string) error {
	if provider == "aliyun" {
		c.rotated = token
	}
	return nil
}

// baseConfig 构造仅含阿里字段的最小配置(Open 接口字段留空,转存用不到)。
func baseConfig(refreshToken string) config.Config {
	return config.Config{
		AliyunRefreshToken: refreshToken,
		AliyunTempFolderID: "root",
		AliyunAPIBase:      aliAPIBase,
		AliyunAuthBase:     aliAuthBase,
		AliyunOpenBase:     aliOpenBase,
		AliyunUserBase:     aliUserBase,
		AliyunBrowserUA:    aliBrowserUA,
	}
}

func shareFromEnv() cache.ShareRef {
	return cache.ShareRef{
		Provider: "aliyun",
		ShareURL: os.Getenv("ALIYUN_TEST_SHARE_URL"),
		SharePwd: os.Getenv("ALIYUN_TEST_SHARE_PWD"),
		FilePath: os.Getenv("ALIYUN_TEST_FILE_PATH"),
	}
}

// listfile 递归收集分享内 subPath 下的所有条目(含子目录里的),直到走完。
// 只读、匿名,不需要 token。depth 从 0 起;maxDepth 防止异常深度跑飞。
//
// 注:每进一层是用 subPath 调 ListShare,内部会从根重新解析一次路径,
// 深层大分享会偏慢。够用;要更快可改用内部 listShare(按 file_id 递归)。
func listfile(ctx context.Context, a *Aliyun, share cache.ShareRef, subPath string, depth, maxDepth int) ([]cache.ShareEntry, error) {
	if depth > maxDepth {
		return nil, nil
	}
	entries, err := a.ListShare(ctx, share, subPath)
	if err != nil {
		return nil, err
	}
	// log.Printf("entries len %d", len(entries))
	all := make([]cache.ShareEntry, 0, len(entries))
	// log.Printf("递归列出分享 %s%s 共 %d 项(深度 %d/%d):", share.ShareURL, subPath, len(entries), depth, maxDepth)
	for _, e := range entries {
		all = append(all, e)
		if e.IsDir {
			sub, err := listfile(ctx, a, share, e.Path, depth+1, maxDepth)
			if err != nil {
				// 单个子目录失败不致命,跳过继续走其余。
				continue
			}
			all = append(all, sub...)
		}
	}
	return all, nil
}

// listfileByID 快速版:按 file_id 递归,只换一次 share_token,不再每层从根解析路径。
// parentID 为当前目录的 file_id(根用 "root");prefix 为当前目录的路径前缀(拼完整路径用)。
// API 调用量 = 目录数(每个目录列一次);而 listfile 是 目录数 × 深度(每层重解析路径)。
func listfileByID(ctx context.Context, a *Aliyun, shareID, shareTok, parentID, prefix string, depth, maxDepth int) ([]cache.ShareEntry, error) {
	if depth > maxDepth {
		return nil, nil
	}
	// log.Printf("递归列出分享 %s/%s  共 %d 项(深度 %d/%d):", shareID, prefix, 0, depth, maxDepth)
	items, err := a.listShare(ctx, shareID, shareTok, parentID)
	if err != nil {
		return nil, err
	}
	all := make([]cache.ShareEntry, 0, len(items))
	// log.Printf("列出目录 %s/%s (parentID=%s) 的条目 %d 个:", shareID, prefix, parentID, len(items))
	for _, it := range items {
		p := prefix + "/" + it.Name
		isDir := it.Type == "folder"
		all = append(all, cache.ShareEntry{Name: it.Name, Path: p, IsDir: isDir, Size: it.Size})
		if isDir {
			sub, err := listfileByID(ctx, a, shareID, shareTok, it.FileID, p, depth+1, maxDepth)
			if err != nil {
				continue // 单个子目录失败不致命,继续走其余。
			}
			all = append(all, sub...)
		}
	}
	return all, nil
}

// TestAllAliyunShareElementsFast 快速版全量遍历:按 file_id 递归。
// 大分享比 TestAllAliyunShareElements 明显快(少了每层从根重解析路径)。
//
//	ALIYUN_TEST_SHARE_URL=https://www.alipan.com/s/xxxx \
//	ALIYUN_TEST_SHARE_PWD=提取码 \
//	go test ./internal/cache/backends/ -run TestAllAliyunShareElementsFast -v -timeout 20m
func TestAllAliyunShareElementsFast(t *testing.T) {
	share := shareFromEnv()
	if share.ShareURL == "" {
		t.Skip("跳过:未设置 ALIYUN_TEST_SHARE_URL")
	}
	a := NewAliyun(baseConfig(""), &captureTokenStore{})
	ctx := context.Background()

	// ① 解析 share_id,② 换一次 share_token(全程复用)。
	shareID := parseAliShareID(share.ShareURL)
	if shareID == "" {
		t.Fatalf("无法解析 share_id: %s", share.ShareURL)
	}
	shareTok, err := a.shareToken(ctx, shareID, share.SharePwd)
	if err != nil {
		t.Fatalf("拿 share_token 失败: %v", err)
	}

	t.Logf("shareID: %s, shareTok: %s", shareID, shareTok)

	// 起点:默认分享根;若给了 SUBPATH 就先解析成 file_id 再从那递归。
	startID, prefix := "root", ""
	if sp := strings.TrimRight(os.Getenv("ALIYUN_TEST_SUBPATH"), "/"); sp != "" {
		id, _, err := a.resolveFileID(ctx, shareID, shareTok, sp)
		if err != nil {
			t.Fatalf("解析起始目录 %q 失败: %v", sp, err)
		}
		t.Logf("解析起始目录 %q 得到 file_id: %s", sp, id)
		startID, prefix = id, sp
	}

	start := time.Now()
	all, err := listfileByID(ctx, a, shareID, shareTok, startID, prefix, 0, 100)
	if err != nil {
		t.Fatalf("遍历失败: %v", err)
	}

	t.Logf("遍历结果: %v", len(all))

	var dirs, files int
	var total int64
	for _, e := range all {
		if e.IsDir {
			dirs++
			// t.Logf("📁 %s", e.Path)
		} else {
			files++
			total += e.Size
			t.Logf("📄 %s  (%d bytes)", e.Path, e.Size)
		}
	}
	t.Logf("──────── 遍历完成(快速版,按 file_id 递归)────────")
	t.Logf("目录 %d，文件 %d，总大小 %.2f GB，耗时 %s",
		dirs, files, float64(total)/(1<<30), time.Since(start).Round(time.Millisecond))
	t.Log("提示:每个 📄 后的路径可直接填给 ALIYUN_TEST_FILE_PATH 去转存。")
}

// TestAliyunShareCount 只数「文件/目录/总大小」,不打印每个文件 —— 最快拿总数。
// 并发列目录(默认 8 路并行),只需分享链接+提取码,不需要 token、不改数据。
//
//	ALIYUN_TEST_SHARE_URL=https://www.alipan.com/s/xxxx \
//	ALIYUN_TEST_SHARE_PWD=提取码 \
//	ALIYUN_TEST_CONCURRENCY=8 \      # 可选,并发数,默认 8
//	go test ./internal/cache/backends/ -run TestAliyunShareCount -v -timeout 20m
func TestAliyunShareCount(t *testing.T) {
	share := shareFromEnv()
	if share.ShareURL == "" {
		t.Skip("跳过:未设置 ALIYUN_TEST_SHARE_URL")
	}
	a := NewAliyun(baseConfig(""), &captureTokenStore{})
	ctx := context.Background()

	shareID := parseAliShareID(share.ShareURL)
	if shareID == "" {
		t.Fatalf("无法解析 share_id: %s", share.ShareURL)
	}
	shareTok, err := a.shareToken(ctx, shareID, share.SharePwd)
	if err != nil {
		t.Fatalf("拿 share_token 失败: %v", err)
	}

	startID := "root"
	if sp := strings.TrimRight(os.Getenv("ALIYUN_TEST_SUBPATH"), "/"); sp != "" {
		id, _, err := a.resolveFileID(ctx, shareID, shareTok, sp)
		if err != nil {
			t.Fatalf("解析起始目录 %q 失败: %v", sp, err)
		}
		startID = id
	}

	conc := 1
	if v := os.Getenv("ALIYUN_TEST_CONCURRENCY"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			conc = n
		}
	}

	var files, dirs, total, listErrs int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, conc) // 并发上限

	var walk func(parentID string)
	walk = func(parentID string) {
		defer wg.Done()
		sem <- struct{}{} // 占一个并发槽
		items, err := a.listShare(ctx, shareID, shareTok, parentID)
		<-sem // 放回(spawn 子目录在锁外,避免父等子导致死锁)
		if err != nil {
			atomic.AddInt64(&listErrs, 1)
			return
		}
		for _, it := range items {
			if it.Type == "folder" {
				atomic.AddInt64(&dirs, 1)
				wg.Add(1)
				go walk(it.FileID)
			} else {
				atomic.AddInt64(&files, 1)
				atomic.AddInt64(&total, it.Size)
			}
		}
	}

	start := time.Now()
	wg.Add(1)
	go walk(startID)
	wg.Wait()

	t.Logf("并发 %d 路,耗时 %s", conc, time.Since(start).Round(time.Millisecond))
	t.Logf("文件 %d 个，目录 %d 个，总大小 %.2f GB", files, dirs, float64(total)/(1<<30))
	if listErrs > 0 {
		t.Logf("有 %d 个目录列取失败(结果可能偏少)", listErrs)
	}
}

// TestAliyunListShare 只读:列出分享内某目录。只需分享链接,不需要 token,不改数据。
func TestAliyunListShare(t *testing.T) {
	share := shareFromEnv()
	if share.ShareURL == "" {
		t.Skip("跳过:未设置 ALIYUN_TEST_SHARE_URL")
	}
	a := NewAliyun(baseConfig(""), &captureTokenStore{})

	subPath := os.Getenv("ALIYUN_TEST_SUBPATH")
	entries, err := a.ListShare(context.Background(), share, subPath)
	if err != nil {
		t.Fatalf("ListShare 失败: %v", err)
	}
	t.Logf("分享 %q 下 %q 共 %d 项:", share.ShareURL, subPath, len(entries))
	t.Logf("列出的条目: %v", entries)

	for i, e := range entries {
		if i >= 30 {
			t.Logf("  …(其余 %d 项省略)", len(entries)-30)
			break
		}
		kind := "文件"
		if e.IsDir {
			kind = "目录"
		}
		t.Logf("  [%s] %-40s size=%d  path=%s", kind, e.Name, e.Size, e.Path)
	}
	if len(entries) == 0 {
		t.Log("该目录为空(或分享/提取码不对)")
	}
}

// TestAllAliyunShareElements 用分享链接+提取码,递归拿到分享内的【全部】文件/目录。
// 只读、匿名、不改任何数据。可选 ALIYUN_TEST_SUBPATH 从某子目录开始。
//
//	ALIYUN_TEST_SHARE_URL=https://www.alipan.com/s/xxxx \
//	ALIYUN_TEST_SHARE_PWD=提取码 \
//	go test ./internal/cache/backends/ -run TestAllAliyunShareElements -v -timeout 20m
func TestAllAliyunShareElements(t *testing.T) {
	share := shareFromEnv()
	if share.ShareURL == "" {
		t.Skip("跳过:未设置 ALIYUN_TEST_SHARE_URL")
	}
	a := NewAliyun(baseConfig(""), &captureTokenStore{})

	root := os.Getenv("ALIYUN_TEST_SUBPATH")
	start := time.Now()
	all, err := listfile(context.Background(), a, share, root, 0, 100)
	if err != nil {
		t.Fatalf("遍历分享失败: %v", err)
	}

	// t.Logf("遍历结果: %v", all)

	var dirs, files int
	var total int64
	for _, e := range all {
		if e.IsDir {
			dirs++
			t.Logf("📁 %s", e.Path)
		} else {
			files++
			total += e.Size
			t.Logf("📄 %s  (%d bytes)", e.Path, e.Size)
		}
	}
	t.Logf("──────── 遍历完成 ────────")
	t.Logf("目录 %d，文件 %d，总大小 %.2f GB，耗时 %s",
		dirs, files, float64(total)/(1<<30), time.Since(start).Round(time.Millisecond))
	t.Log("提示:上面每个 📄 后的路径,可直接填给 ALIYUN_TEST_FILE_PATH 去转存。")
}

// TestAliyunSaveToFolder 真转存:把分享内某文件秒转存到你自己盘。
// 必须显式 ALIYUN_TEST_DO_TRANSFER=1(会写入你网盘、轮换你的 token)。
func TestAliyunSaveToFolder(t *testing.T) {
	if os.Getenv("ALIYUN_TEST_DO_TRANSFER") != "1" {
		t.Skip("跳过:真转存需显式 ALIYUN_TEST_DO_TRANSFER=1(会写入你的网盘、轮换 token)")
	}
	rt := os.Getenv("ALIYUN_TEST_REFRESH_TOKEN")
	if rt == "" {
		t.Skip("跳过:未设置 ALIYUN_TEST_REFRESH_TOKEN(网页版 refresh_token)")
	}
	share := shareFromEnv()
	if share.ShareURL == "" || share.FilePath == "" {
		t.Fatal("需要 ALIYUN_TEST_SHARE_URL 和 ALIYUN_TEST_FILE_PATH(要转存的文件路径)")
	}
	target := os.Getenv("ALIYUN_TEST_TARGET")
	if target == "" {
		target = "ivideo"
	}

	ts := &captureTokenStore{web: rt}
	a := NewAliyun(baseConfig(rt), ts)

	t.Logf("开始转存: %s (文件 %s) → 自己盘 /%s", share.ShareURL, share.FilePath, target)
	start := time.Now()
	err := a.SaveToFolder(context.Background(), share, share.FilePath, target)
	elapsed := time.Since(start).Round(time.Millisecond)

	// 无论成败,先提示 token 是否被轮换(阿里刷新即换新值)。
	defer func() {
		if ts.rotated != "" && ts.rotated != rt {
			t.Logf("⚠️ 你的 web refresh_token 已被阿里轮换,旧值作废。下次请用新值:")
			t.Logf("   新 token = %s", ts.rotated)
			t.Logf("   ⚠️ ivideo 库里存的旧 token 现在也失效了 —— 需要用此新值更新库,或重新扫码。")
		}
	}()

	if err != nil {
		t.Fatalf("SaveToFolder 失败(耗时 %s): %v", elapsed, err)
	}
	t.Logf("✅ 转存成功,耗时 %s", elapsed)
	t.Log("   秒转存判定:耗时应为秒级、且与文件大小无关(阿里云内部盘到盘复制,0 带宽)")
	t.Logf("   现在去自己盘 /%s 里应能看到该文件", target)
}
