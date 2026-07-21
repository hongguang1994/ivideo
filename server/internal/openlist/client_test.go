package openlist

// 针对真实 OpenList 的集成测试。
//
// 默认跳过;只有设置了 OPENLIST_TEST_URL 才会真正连服务器,
// 这样本地 `go test ./...` 和 CI 不会误连内网。
//
// 本地对着服务器上的 openlist 跑(免部署):
//
//	OPENLIST_TEST_URL=http://192.168.50.140:5244 \
//	OPENLIST_TEST_PASS=ivideo123 \
//	go test ./internal/openlist/ -run TestClient -v
//
// 用户名默认 admin、密码默认 ivideo123,可用 OPENLIST_TEST_USER /
// OPENLIST_TEST_PASS 覆盖。

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTestClient 从环境变量构造 client;未配置 URL 则跳过整条用例。
func newTestClient(t *testing.T) *Client {
	t.Helper()
	base := os.Getenv("OPENLIST_TEST_URL")
	if base == "" {
		t.Skip("跳过:未设置 OPENLIST_TEST_URL(集成测试需连真实 openlist)")
	}
	user := os.Getenv("OPENLIST_TEST_USER")
	if user == "" {
		user = "admin"
	}
	pass := os.Getenv("OPENLIST_TEST_PASS")
	if pass == "" {
		pass = "ivideo123"
	}
	return New(base, user, pass)
}

// TestClientLogin 验证能登录并拿到非空 token。
func TestClientLogin(t *testing.T) {
	c := newTestClient(t)

	tok, err := c.getToken(false)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if tok == "" {
		t.Fatal("登录成功但拿到空 token")
	}
	t.Logf("登录成功,token 前缀: %.12s…(len=%d)", tok, len(tok))

	// 缓存应生效:再取一次不该重新登录(值一致)。
	tok2, err := c.getToken(false)
	if err != nil {
		t.Fatalf("二次取 token 失败: %v", err)
	}
	if tok2 != tok {
		t.Error("getToken(false) 未命中缓存,两次 token 不一致")
	}
}

// TestClientList 列根目录,打印挂载/条目。
func TestClientList(t *testing.T) {
	c := newTestClient(t)

	items, err := c.List("/")
	if err != nil {
		t.Fatalf("List(\"/\") 失败: %v", err)
	}
	t.Logf("根目录共 %d 项:", len(items))
	for _, it := range items {
		kind := "文件"
		if it.IsDir {
			kind = "目录"
		}
		t.Logf("  [%s] %-30s size=%d type=%d", kind, it.Name, it.Size, it.Type)
	}
	if len(items) == 0 {
		t.Skip("根目录为空(openlist 还没挂任何存储),后续依赖内容的用例跳过")
	}
}

// TestClientListSubDir 进入根下第一个目录,验证能逐层浏览。
func TestClientListSubDir(t *testing.T) {
	c := newTestClient(t)

	root, err := c.List("/")
	if err != nil {
		t.Fatalf("List(\"/\") 失败: %v", err)
	}
	var firstDir string
	for _, it := range root {
		if it.IsDir {
			firstDir = it.Name
			break
		}
	}
	if firstDir == "" {
		t.Skip("根下没有子目录,跳过")
	}

	sub := "/" + firstDir
	items, err := c.List(sub)
	if err != nil {
		t.Fatalf("List(%q) 失败: %v", sub, err)
	}
	t.Logf("%s 下共 %d 项", sub, len(items))
	for i, it := range items {
		if i >= 10 {
			t.Logf("  …(其余 %d 项省略)", len(items)-10)
			break
		}
		t.Logf("  %s (dir=%v)", it.Name, it.IsDir)
	}
}

// TestClientRawURL 尽力找一个文件取直链。
// 分享盘文件可能取不到可播直链(返回空),这里只做“尽力”验证、不硬性失败。
func TestClientRawURL(t *testing.T) {
	c := newTestClient(t)

	path, ok := findFirstFile(t, c, "/", 3)
	if !ok {
		t.Skip("没找到文件(可能全是空挂载),跳过")
	}

	raw, err := c.RawURL(path)
	if err != nil {
		// 分享盘常见:能列不能取直链。记录而非失败,便于观察真实行为。
		t.Logf("RawURL(%q) 报错(分享盘常见): %v", path, err)
		return
	}
	if raw == "" {
		t.Logf("RawURL(%q) 返回空(该存储不发直链)", path)
		return
	}
	t.Logf("RawURL(%q) = %.80s…", path, raw)
}

// findFirstFile 从 dir 起,最多下钻 depth 层,返回第一个文件的完整路径。
func findFirstFile(t *testing.T, c *Client, dir string, depth int) (string, bool) {
	t.Helper()
	if depth < 0 {
		return "", false
	}
	items, err := c.List(dir)
	if err != nil {
		t.Logf("List(%q) 失败: %v", dir, err)
		return "", false
	}
	prefix := dir
	if prefix != "/" {
		prefix += "/"
	}
	// 先找当前层的文件。
	for _, it := range items {
		if !it.IsDir {
			return prefix + it.Name, true
		}
	}
	// 再下钻目录。
	for _, it := range items {
		if it.IsDir {
			if p, ok := findFirstFile(t, c, prefix+it.Name, depth-1); ok {
				return p, true
			}
		}
	}
	return "", false
}

// TestClientWalkAll 递归遍历整棵目录树,直到走完为止,打印结构并汇总统计。
//
// ⚠️ 分享盘目录可能很深、文件可能上万,这个用例会比较慢、输出很大。
// 建议单独跑,并放宽超时:
//
//	OPENLIST_TEST_URL=http://192.168.50.140:5244 \
//	OPENLIST_TEST_PASS=ivideo123 \
//	go test ./internal/openlist/ -run TestClientWalkAll -v -timeout 30m
//
// 可选环境变量:
//   - OPENLIST_TEST_WALK_ROOT      从哪个目录开始(默认 "/")
//   - OPENLIST_TEST_WALK_MAXDEPTH  最大下钻深度,防跑飞(默认 100)
//   - OPENLIST_TEST_WALK_MAXITEMS  最多遍历多少个条目就停(默认 0=不限)
func TestClientWalkAll(t *testing.T) {
	c := newTestClient(t)

	root := envOr("OPENLIST_TEST_WALK_ROOT", "/")
	maxDepth := envInt("OPENLIST_TEST_WALK_MAXDEPTH", 100)
	maxItems := envInt("OPENLIST_TEST_WALK_MAXITEMS", 0)

	t.Logf("开始遍历: root=%q maxDepth=%d maxItems=%d", root, maxDepth, maxItems)
	start := time.Now()

	w := &treeWalker{t: t, c: c, maxDepth: maxDepth, maxItems: maxItems}
	w.walk(root, 0)

	t.Logf("──────────── 遍历结束 ────────────")
	t.Logf("目录: %d   文件: %d   总大小: %s", w.dirs, w.files, humanSize(w.totalSize))
	if w.listErrors > 0 {
		t.Logf("列目录失败: %d 处(已跳过对应子树)", w.listErrors)
	}
	if w.stopped {
		t.Logf("⚠️  达到 maxItems=%d 上限,提前停止(未走完整棵树)", maxItems)
	}
	t.Logf("耗时: %s", time.Since(start).Round(time.Millisecond))
}

// treeWalker 持有遍历过程中的统计与限制。
type treeWalker struct {
	t        *testing.T
	c        *Client
	maxDepth int
	maxItems int // 0 表示不限

	dirs       int
	files      int
	totalSize  int64
	listErrors int
	visited    int
	stopped    bool
}

// walk 递归遍历 dir。depth 从 0 起。
func (w *treeWalker) walk(dir string, depth int) {
	if w.stopped {
		return
	}
	if depth > w.maxDepth {
		w.t.Logf("%s⚠️ 达到 maxDepth=%d,停止下钻: %s", indent(depth), w.maxDepth, dir)
		return
	}

	items, err := w.listWithRetry(dir, 3)
	if err != nil {
		w.listErrors++
		w.t.Logf("%s❌ 列目录失败 %s: %v", indent(depth), dir, err)
		return
	}

	prefix := dir
	if prefix != "/" {
		prefix += "/"
	}

	for _, it := range items {
		if w.maxItems > 0 && w.visited >= w.maxItems {
			w.stopped = true
			return
		}
		w.visited++

		if it.IsDir {
			w.dirs++
			w.t.Logf("%s📁 %s", indent(depth), it.Name)
			w.walk(prefix+it.Name, depth+1)
			if w.stopped {
				return
			}
		} else {
			w.files++
			w.totalSize += it.Size
			w.t.Logf("%s📄 %s (%s)", indent(depth), it.Name, humanSize(it.Size))
		}
	}
}

// listWithRetry 列目录,失败重试(分享盘偶发抖动时更稳)。
func (w *treeWalker) listWithRetry(dir string, attempts int) ([]FileItem, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		items, err := w.c.List(dir)
		if err == nil {
			return items, nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return nil, lastErr
}

// ---- 小工具 ----

func indent(depth int) string { return strings.Repeat("  ", depth) }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// humanSize 把字节数格式化成人类可读(B/KB/MB/…)。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
