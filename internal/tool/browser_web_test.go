package tool

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const webSelectFixture = `<html><body>
<ul>
  <li><a href="/a" title="first">Alpha</a></li>
  <li><a href="/b" title="second">Beta</a></li>
  <li><a>NoLink</a></li>
</ul>
<table><tr><td class="empty">   </td></tr></table>
</body></html>`

func parseFixture(t *testing.T) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(webSelectFixture))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// attr:"text" 曾被当成"读取名为 text 的属性"，静默返回空串。
func TestWebSelectAttrTextReturnsElementText(t *testing.T) {
	doc := parseFixture(t)
	res := applySelectors(doc, []interface{}{
		map[string]interface{}{"name": "title", "selector": "a", "attr": "text"},
		map[string]interface{}{"name": "inner", "selector": "a", "attr": "inner_text"},
	})
	if res["title"] != "Alpha" {
		t.Fatalf("attr=text: got %q, want %q", res["title"], "Alpha")
	}
	if res["inner"] != "Alpha" {
		t.Fatalf("attr=inner_text: got %q", res["inner"])
	}
}

func TestWebSelectAttributeValue(t *testing.T) {
	doc := parseFixture(t)
	res := applySelectors(doc, []interface{}{
		map[string]interface{}{"name": "href", "selector": "a", "attr": "href"},
	})
	if res["href"] != "/a" {
		t.Fatalf("href: got %v", res["href"])
	}
}

// 多元素匹配时默认仍取第一个，但 matched 必须给出真实数量。
func TestWebSelectReportsMatchCount(t *testing.T) {
	doc := parseFixture(t)
	res := applySelectors(doc, []interface{}{
		map[string]interface{}{"name": "href", "selector": "a", "attr": "href"},
	})
	matched, ok := res["matched"].(map[string]int)
	if !ok {
		t.Fatalf("missing matched map: %v", res)
	}
	if matched["href"] != 3 {
		t.Fatalf("matched[href]: got %d, want 3", matched["href"])
	}
}

// all=true 返回全部取值（提取链接列表），limit 截断。
func TestWebSelectAllReturnsEveryMatch(t *testing.T) {
	doc := parseFixture(t)
	res := applySelectors(doc, []interface{}{
		map[string]interface{}{"name": "links", "selector": "a[href]", "attr": "href", "all": true},
		map[string]interface{}{"name": "one", "selector": "a[href]", "attr": "href", "all": true, "limit": 1},
	})

	links, ok := res["links"].([]string)
	if !ok || len(links) != 2 || links[0] != "/a" || links[1] != "/b" {
		t.Fatalf("links: got %#v", res["links"])
	}
	one, ok := res["one"].([]string)
	if !ok || len(one) != 1 {
		t.Fatalf("limit: got %#v", res["one"])
	}
}

// 未命中必须有可区分的信号，而不是只回一个空字符串。
func TestWebSelectNoMatchIsDistinguishable(t *testing.T) {
	doc := parseFixture(t)
	res := applySelectors(doc, []interface{}{
		map[string]interface{}{"name": "missing", "selector": "div.nope"},
	})
	if res["missing"] != "" {
		t.Fatalf("missing should be empty string, got %v", res["missing"])
	}
	if res["matched"].(map[string]int)["missing"] != 0 {
		t.Fatalf("matched[missing] should be 0")
	}
	note, _ := res["note"].(string)
	if !strings.Contains(note, "missing") {
		t.Fatalf("note should list the unmatched selector, got %q", note)
	}
}

func TestDecodeHTMLBodyGBK(t *testing.T) {
	const text = "中文标题"
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("<html><head><meta charset=\"gbk\"></head><body>" + text + "</body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeHTMLBody(gbk, "text/html")
	if !strings.Contains(string(decoded), text) {
		t.Fatalf("GBK body not decoded: %q", string(decoded))
	}
}

func TestDecodeHTMLBodyFromContentType(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("内容"))
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeHTMLBody(gbk, "text/html; charset=GB2312")
	if string(decoded) != "内容" {
		t.Fatalf("charset from header not honored: %q", string(decoded))
	}
}

func TestDecodeHTMLBodyUTF8PassThrough(t *testing.T) {
	body := []byte("<html><body>中文</body></html>")
	if got := string(decodeHTMLBody(body, "text/html; charset=utf-8")); got != string(body) {
		t.Fatalf("utf-8 body must pass through untouched: %q", got)
	}
}
