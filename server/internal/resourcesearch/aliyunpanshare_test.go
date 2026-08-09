package resourcesearch

import "testing"

func TestParseAliyunPanShareMarkdown(t *testing.T) {
	content := `资源类型 | 资源名称 | 文件名称 | 分享链接 | 提取码 | 更新时间
	电视剧 | 成名在望2026 | The.Fame.S01E03.mkv | https://pan.quark.cn/s/abc123 | 74jw | 2026-08-02 21:03:13
	电影 | 其他资源 | 01.mp4 | https://www.alipan.com/s/def456 | 2026-08-02`
	results := parseAliyunPanShareMarkdown(content, "成名在望", "今日更新合集.md")
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	got := results[0]
	if got.Provider != "quark" || got.SharePwd != "74jw" || got.Title != "成名在望2026" || got.ResourceType != "电视剧" || got.FileName != "The.Fame.S01E03.mkv" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestParseAliyunPanShareMarkdownMatchesAnyCell(t *testing.T) {
	content := `资源类型 | 资源名称 | 文件名称 | 分享链接
电视剧 | 热门剧 | S01E01.mkv | https://www.aliyundrive.com/s/abc123`
	results := parseAliyunPanShareMarkdown(content, "S01E01", "a.md")
	if len(results) != 1 || results[0].Provider != "aliyun" {
		t.Fatalf("unexpected results: %+v", results)
	}
}
