package strmscrape

import (
	"context"
	"path/filepath"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/strm"
)

func TestSetTVStateTogglesEndedAndUpdating(t *testing.T) {
	strmRoot := t.TempDir()
	outputFolder := "完结切换"
	root := strm.TaskOutputDir(strmRoot, outputFolder)
	show := filepath.Join(root, "测试剧集 (2020)")
	season := filepath.Join(show, "Season 01")
	mustMkdir(t, season)
	mustWrite(t, filepath.Join(season, "测试剧集.S01E01.strm"), "x")
	mustWrite(t, filepath.Join(show, "tvshow.nfo"), "<tvshow><title>测试剧集</title><year>2020</year><tmdbid>1234</tmdbid></tvshow>\n")
	mustWrite(t, filepath.Join(show, "poster.jpg"), "image")
	works, err := scanWorks(root)
	if err != nil {
		t.Fatal(err)
	}
	task := &domain.StrmTask{ID: 11, OutputFolder: outputFolder}
	strmSvc := strm.NewService(strm.ServiceOptions{Repo: &rematchTaskRepo{task: task}, StrmDir: strmRoot})
	svc := New(Options{Strm: strmSvc, StrmDir: strmRoot, DataDir: t.TempDir()})
	itemID := pathToItemID(works[0].relKey)

	item, err := svc.SetTVState(context.Background(), SetTVStateRequest{StrmTaskID: 11, ItemID: itemID, Ended: true})
	if err != nil {
		t.Fatal(err)
	}
	if item.TVState != TVStateEnded {
		t.Fatalf("设为完结后 tv_state 应为 ended：%+v", item)
	}

	item, err = svc.SetTVState(context.Background(), SetTVStateRequest{StrmTaskID: 11, ItemID: itemID, Ended: false})
	if err != nil {
		t.Fatal(err)
	}
	if item.TVState != TVStateUpdating {
		t.Fatalf("取消完结后 tv_state 应为 updating：%+v", item)
	}

	item, err = svc.SetTVState(context.Background(), SetTVStateRequest{StrmTaskID: 11, ItemID: itemID, Ended: true})
	if err != nil {
		t.Fatal(err)
	}
	if item.TVState != TVStateEnded {
		t.Fatalf("再次设为完结后 tv_state 应为 ended：%+v", item)
	}
}
