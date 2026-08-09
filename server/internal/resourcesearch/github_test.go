package resourcesearch

import (
	"strings"
	"testing"
)

func TestExtractShareResults(t *testing.T) {
	content := `凡人修仙传
阿里：https://www.alipan.com/s/abc_123 提取码：9x2a
夸克 https://pan.quark.cn/s/xyz987 访问码: qwer
重复 https://www.alipan.com/s/abc_123`
	results := extractShareResults(content, "凡人修仙传")
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Provider != "aliyun" || results[0].SharePwd != "9x2a" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[1].Provider != "quark" || results[1].SharePwd != "qwer" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestDetectProviderRejectsLookalikeHost(t *testing.T) {
	if got := detectProvider("https://alipan.com.example.org/s/abc"); got != "" {
		t.Fatalf("lookalike host accepted as %q", got)
	}
}

func TestExtractMatchingShareResultsExcludesOtherRows(t *testing.T) {
	content := `| 片名 | 链接 |
| --- | --- |
| 霍比特人3 | https://www.alipan.com/s/hobbit |
| 其他电影 | https://pan.quark.cn/s/other |
| 又一电影 | https://115.com/s/another |`
	results := extractMatchingShareResults(content, "霍比特人3")
	if len(results) != 1 || results[0].ShareURL != "https://www.alipan.com/s/hobbit" {
		t.Fatalf("unexpected contextual results: %#v", results)
	}
}

func TestExtractMatchingShareResultsUsesNearbyTitle(t *testing.T) {
	content := `霍比特人3：五军之战
资源链接
https://www.alipan.com/s/hobbit

无关内容
https://pan.quark.cn/s/other`
	results := extractMatchingShareResults(content, "霍比特人3")
	if len(results) != 1 || results[0].Provider != "aliyun" {
		t.Fatalf("nearby title was not used: %#v", results)
	}
}

func TestBuildGitHubCodeQueryTargetsSupportedShareDomains(t *testing.T) {
	query := buildGitHubCodeQuery("师兄啊师兄")
	for _, expected := range []string{`"师兄啊师兄"`, "alipan.com/s/", "aliyundrive.com/s/", "pan.quark.cn/s/", "115.com/s/", "NOT is:fork"} {
		if !strings.Contains(query, expected) {
			t.Fatalf("query missing %q: %s", expected, query)
		}
	}
}
