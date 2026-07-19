package backends

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"sync"
	"time"

	"ivideo/server/internal/cache"
)

// 一个公开可访问、支持 Range 的示例视频，让整条“转存→就绪→播放”链路本地就能跑通。
const fakeSampleURL = "https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4"

// Fake 是用于本地联调的假适配器：不真的转存，只模拟状态流转与耗时。
type Fake struct {
	mu    sync.Mutex
	files map[string]int64 // cache_path -> size，模拟自己网盘里的文件
}

// NewFake 创建假适配器。
func NewFake() *Fake {
	return &Fake{files: make(map[string]int64)}
}

func (f *Fake) Name() string { return "fake" }

// Transfer 模拟一次转存：短暂延时后“落盘”，返回固定大小。
func (f *Fake) Transfer(ctx context.Context, share cache.ShareRef) (cache.TransferResult, error) {
	select {
	case <-time.After(800 * time.Millisecond): // 模拟秒传级耗时
	case <-ctx.Done():
		return cache.TransferResult{}, ctx.Err()
	}
	sum := sha1.Sum([]byte(share.ShareURL + share.FilePath))
	path := "/fake-cache/" + hex.EncodeToString(sum[:8])
	const size = int64(158 * 1024 * 1024) // 假设 158MB

	f.mu.Lock()
	f.files[path] = size
	f.mu.Unlock()
	return cache.TransferResult{CachePath: path, Size: size}, nil
}

// DirectURL 返回可播直链（示例视频）。
func (f *Fake) DirectURL(ctx context.Context, cachePath string) (string, error) {
	return fakeSampleURL, nil
}

// Delete 从模拟网盘移除文件。
func (f *Fake) Delete(ctx context.Context, cachePath string) error {
	f.mu.Lock()
	delete(f.files, cachePath)
	f.mu.Unlock()
	return nil
}

// EmptyTrash 假适配器无回收站，空操作。
func (f *Fake) EmptyTrash(ctx context.Context) error { return nil }

// Quota 返回模拟已用量。
func (f *Fake) Quota(ctx context.Context) (used, total int64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.files {
		used += s
	}
	return used, 2 * 1024 * 1024 * 1024 * 1024, nil // 2TB 总量
}
