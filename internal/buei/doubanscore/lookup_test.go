package doubanscore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 用 httptest 覆盖查找链路：搜索页被风控时仍要能拿到评分。

type lookupServer struct {
	srv       *httptest.Server
	searches  atomic.Int32
	rexxar    atomic.Int32
	suggests  atomic.Int32
	details   atomic.Int32
	searchFn  func(w http.ResponseWriter, r *http.Request)
	rexxarFn  func(w http.ResponseWriter, r *http.Request)
	suggestFn func(w http.ResponseWriter, r *http.Request)
	detailFn  func(w http.ResponseWriter, r *http.Request)
}

func newLookupServer(t *testing.T) *lookupServer {
	t.Helper()
	ls := &lookupServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/subject_search", func(w http.ResponseWriter, r *http.Request) {
		ls.searches.Add(1)
		if ls.searchFn != nil {
			ls.searchFn(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>没有数据</body></html>"))
	})
	mux.HandleFunc("/rexxar/api/v2/search/subjects", func(w http.ResponseWriter, r *http.Request) {
		ls.rexxar.Add(1)
		if ls.rexxarFn != nil {
			ls.rexxarFn(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"subjects":{"items":[]}}`))
	})
	mux.HandleFunc("/rexxar/api/v2/movie/", func(w http.ResponseWriter, r *http.Request) {
		ls.details.Add(1)
		if ls.detailFn != nil {
			ls.detailFn(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/j/subject_suggest", func(w http.ResponseWriter, r *http.Request) {
		ls.suggests.Add(1)
		if ls.suggestFn != nil {
			ls.suggestFn(w, r)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})
	ls.srv = httptest.NewServer(mux)
	t.Cleanup(ls.srv.Close)
	return ls
}

func newTestService(ls *lookupServer) *Service {
	cli := newDoubanClient(time.Millisecond)
	cli.searchBase = ls.srv.URL
	cli.movieBase = ls.srv.URL
	cli.mobileBase = ls.srv.URL
	return &Service{douban: cli}
}

func TestLookupFallsBackToRexxarWhenSearchBlocked(t *testing.T) {
	ls := newLookupServer(t)
	// 搜索页无 __DATA__（被风控/结构变化）
	ls.searchFn = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><head><title>豆瓣</title></head><body>sec challenge</body></html>"))
	}
	ls.rexxarFn = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"subjects":{"items":[
			{"target":{"id":"27166442","title":"夏目友人帐","year":"2018","rating":{"value":8.0,"count":209936}}},
			{"target":{"id":"3060542","title":"夏目友人帐","year":"2008","rating":{"value":9.4,"count":207269}}}
		]}}`))
	}
	svc := newTestService(ls)

	res, err := svc.lookupDouban(context.Background(), "夏目友人帐：结缘空蝉", intPtr(2018), "movie")
	if err != nil {
		t.Fatalf("lookupDouban: %v", err)
	}
	if res == nil || res.Rating == nil || *res.Rating != 8.0 || res.DoubanID != "27166442" || res.Source != "rexxar" {
		t.Fatalf("应从移动端搜索拿到 8.0 分：%+v", res)
	}
	if !svc.douban.htmlSearchBlocked() {
		t.Fatal("搜索页解析失败后应进入冷却，避免每个条目都白跑一次")
	}

	// 冷却期内不再请求搜索页
	before := ls.searches.Load()
	if _, err := svc.lookupDouban(context.Background(), "夏目友人帐", intPtr(2018), "movie"); err != nil {
		t.Fatalf("lookupDouban(second): %v", err)
	}
	if got := ls.searches.Load(); got != before {
		t.Fatalf("冷却期内不应再请求搜索页，实际多发了 %d 次", got-before)
	}
}

func TestLookupUsesSearchWhenAvailable(t *testing.T) {
	ls := newLookupServer(t)
	ls.searchFn = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><script>window.__DATA__ = {"items":[
			{"tpl_name":"search_subject","title":"夏目友人帐‎ (2018)","url":"https://movie.douban.com/subject/27166442/","rating":{"value":8.0,"count":209936},"labels":[],"more_url":""}
		]}</script></html>`))
	}
	svc := newTestService(ls)

	res, err := svc.lookupDouban(context.Background(), "夏目友人帐", intPtr(2018), "movie")
	if err != nil {
		t.Fatalf("lookupDouban: %v", err)
	}
	if res == nil || res.Rating == nil || *res.Rating != 8.0 || res.Source != "search" {
		t.Fatalf("应命中搜索页：%+v", res)
	}
	if ls.rexxar.Load() != 0 || ls.suggests.Load() != 0 {
		t.Fatalf("搜索页成功时不应再请求其它来源")
	}
}

func TestLookupSuggestThenDetail(t *testing.T) {
	ls := newLookupServer(t)
	// 搜索页与移动端搜索都没有结果
	ls.suggestFn = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"27166442","type":"movie","title":"夏目友人帐","sub_title":"劇場版 夏目友人帳 ～うつせみに結ぶ～","year":"2018","episode":"","url":"https://movie.douban.com/subject/27166442/"}]`))
	}
	ls.detailFn = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"27166442","title":"夏目友人帐","year":"2018","type":"movie","is_tv":false,"episodes_count":0,"rating":{"value":8.0,"count":209936}}`))
	}
	svc := newTestService(ls)

	res, err := svc.lookupDouban(context.Background(), "夏目友人帐：结缘空蝉", intPtr(2018), "movie")
	if err != nil {
		t.Fatalf("lookupDouban: %v", err)
	}
	if res == nil || res.Rating == nil || *res.Rating != 8.0 || res.Source != "detail" {
		t.Fatalf("suggest 命中后应补条目详情评分：%+v", res)
	}
	if ls.details.Load() != 1 {
		t.Fatalf("应只请求一次详情，实际 %d 次", ls.details.Load())
	}
}

func TestLookupDetailMarksTV(t *testing.T) {
	ls := newLookupServer(t)
	ls.suggestFn = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"26647087","type":"movie","title":"三体","year":"2023","episode":"30","url":"https://movie.douban.com/subject/26647087/"}]`))
	}
	ls.detailFn = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"26647087","title":"三体","year":"2023","type":"tv","is_tv":true,"episodes_count":30,"rating":{"value":8.8,"count":542187}}`))
	}
	svc := newTestService(ls)

	res, err := svc.lookupDouban(context.Background(), "三体", intPtr(2023), "tv")
	if err != nil {
		t.Fatalf("lookupDouban: %v", err)
	}
	if res == nil || !res.IsTV || res.Rating == nil || *res.Rating != 8.8 {
		t.Fatalf("详情应标明剧集并带评分：%+v", res)
	}
}

func TestLookupAllSourcesFail(t *testing.T) {
	ls := newLookupServer(t)
	// 403 不重试，测试跑得快；真实环境的 5xx 仍会退避重试。
	fail := func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusForbidden) }
	ls.searchFn, ls.rexxarFn, ls.suggestFn = fail, fail, fail
	svc := newTestService(ls)

	if _, err := svc.lookupDouban(context.Background(), "某片", nil, "movie"); err == nil {
		t.Fatal("三个来源都失败时应返回错误，而不是当成没有匹配")
	}
}

func TestParseRexxarDetail(t *testing.T) {
	res, err := parseRexxarDetail("1", []byte(`{"id":"1","title":"某剧","year":"2024","type":"tv","is_tv":true,"episodes_count":12,"rating":{"value":7.5,"count":100}}`))
	if err != nil {
		t.Fatalf("parseRexxarDetail: %v", err)
	}
	if !res.IsTV || res.Rating == nil || *res.Rating != 7.5 || res.DoubanTitle != "某剧" {
		t.Fatalf("详情解析异常：%+v", res)
	}

	res, err = parseRexxarDetail("2", []byte(`{"id":"2","title":"某电影","year":"1999","rating":{"value":0,"count":0}}`))
	if err != nil {
		t.Fatalf("parseRexxarDetail: %v", err)
	}
	if res.IsTV || res.Rating != nil {
		t.Fatalf("无评分电影不应带评分：%+v", res)
	}
}

func TestParseRexxarSearch(t *testing.T) {
	hits, err := parseRexxarSearch([]byte(`{"subjects":{"items":[
		{"target":{"id":"1","title":"甲","year":"2020","rating":{"value":8.2,"count":10}}},
		{"target":{"id":"","title":"乙"}}
	]}}`))
	if err != nil {
		t.Fatalf("parseRexxarSearch: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "1" || hits[0].Rating == nil || *hits[0].Rating != 8.2 {
		t.Fatalf("移动端搜索结果解析异常：%+v", hits)
	}
	if hits[0].Year == nil || *hits[0].Year != 2020 {
		t.Fatalf("年份解析异常：%+v", hits[0])
	}
}

func TestChallengeDetection(t *testing.T) {
	// 搜索页 HTML 变化时也要进入冷却，避免每分钟对豆瓣重试。
	cli := newDoubanClient(time.Millisecond)
	if _, err := decodeDoubanData([]byte("<html>blocked</html>")); err == nil || !strings.Contains(err.Error(), "window.__DATA__") {
		t.Fatalf("应报缺少 window.__DATA__：%v", err)
	}
	if cli.htmlSearchBlocked() {
		t.Fatal("初始状态不应处于冷却")
	}
}
