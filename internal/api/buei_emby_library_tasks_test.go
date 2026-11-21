package api

import (
	"testing"

	"litepan/internal/buei/embyarchive"
	"litepan/internal/domain"
)

func TestBueiMatchStrmTasksArchiveRules(t *testing.T) {
	tasks := []*domain.StrmTask{
		{ID: 1, Name: "夸克电影 · 归档"},
		{ID: 2, Name: "夸克剧集 · 归档"},
		{ID: 3, Name: "夸克电影"},
		{ID: 4, Name: "电影整理"},
	}

	normal := embyarchive.MatchStrmTasks(tasks, "电影")
	if len(normal) != 2 || normal[0].ID != 3 || normal[1].ID != 4 {
		t.Fatalf("普通媒体库匹配异常：%+v", normal)
	}

	archiveMovie := embyarchive.MatchStrmTasks(tasks, "归档 · 电影")
	if len(archiveMovie) != 1 || archiveMovie[0].ID != 1 {
		t.Fatalf("归档电影媒体库匹配异常：%+v", archiveMovie)
	}

	archiveSeries := embyarchive.MatchStrmTasks(tasks, "归档 · 剧集")
	if len(archiveSeries) != 1 || archiveSeries[0].ID != 2 {
		t.Fatalf("归档剧集媒体库匹配异常：%+v", archiveSeries)
	}
}
