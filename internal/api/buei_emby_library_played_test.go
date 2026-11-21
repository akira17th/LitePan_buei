package api

import (
	"testing"

	"litepan/internal/embyproxy"
	"litepan/internal/strmscrape"
)

func TestBueiApplyUserPlayedStatus(t *testing.T) {
	users := []embyproxy.LibraryUserItemStatus{
		{UserID: "u1", WatchedEpisodes: 2, RemainingEpisodes: 8, RemainingKnown: true, TotalEpisodes: 10},
		{UserID: "u2"},
	}
	updated, ok := bueiApplyUserPlayedStatus(users, "u1", strmscrape.Item{MediaType: strmscrape.MediaTypeTV}, 10, true)
	if !ok || !updated.Played || updated.WatchedEpisodes != 10 || updated.RemainingEpisodes != 0 || !updated.RemainingKnown {
		t.Fatalf("标记已看完后剧集进度异常：%+v", updated)
	}
	if users[1].Played {
		t.Fatalf("不应修改其他用户：%+v", users[1])
	}

	updated, ok = bueiApplyUserPlayedStatus(users, "u1", strmscrape.Item{MediaType: strmscrape.MediaTypeTV}, 10, false)
	if !ok || updated.Played || updated.WatchedEpisodes != 0 || updated.RemainingEpisodes != 10 {
		t.Fatalf("取消标记后剧集进度异常：%+v", updated)
	}

	movieUsers := []embyproxy.LibraryUserItemStatus{{UserID: "u1"}}
	updated, ok = bueiApplyUserPlayedStatus(movieUsers, "u1", strmscrape.Item{MediaType: strmscrape.MediaTypeMovie}, 0, true)
	if !ok || !updated.Played || updated.WatchedEpisodes != 1 {
		t.Fatalf("电影标记已看完异常：%+v", updated)
	}

	if _, ok := bueiApplyUserPlayedStatus(users, "u9", strmscrape.Item{}, 0, true); ok {
		t.Fatal("不存在的用户不应更新成功")
	}
}
