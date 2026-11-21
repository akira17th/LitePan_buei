package embyarchive

import (
	"reflect"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

func TestMatchStrmTasksArchiveRules(t *testing.T) {
	tasks := []*domain.StrmTask{
		{ID: 1, Name: "夸克电影 · 归档"},
		{ID: 2, Name: "夸克剧集 · 归档"},
		{ID: 3, Name: "夸克电影"},
		{ID: 4, Name: "电影整理"},
	}

	normal := MatchStrmTasks(tasks, "电影")
	if len(normal) != 2 || normal[0].ID != 3 || normal[1].ID != 4 {
		t.Fatalf("普通媒体库匹配异常：%+v", normal)
	}
	archiveMovie := MatchStrmTasks(tasks, "归档 · 电影")
	if len(archiveMovie) != 1 || archiveMovie[0].ID != 1 {
		t.Fatalf("归档电影媒体库匹配异常：%+v", archiveMovie)
	}
	archiveSeries := MatchStrmTasks(tasks, "归档 · 剧集")
	if len(archiveSeries) != 1 || archiveSeries[0].ID != 2 {
		t.Fatalf("归档剧集媒体库匹配异常：%+v", archiveSeries)
	}
}

func TestMatchStrmTasksUsesTaskNameSubstring(t *testing.T) {
	tasks := []*domain.StrmTask{
		{ID: 1, Name: "电影-国产"},
		{ID: 2, Name: "电影-欧美"},
		{ID: 3, Name: "剧集"},
		{ID: 4, Name: ""},
	}
	matched := MatchStrmTasks(tasks, "电影")
	if len(matched) != 2 || matched[0].ID != 1 || matched[1].ID != 2 {
		t.Fatalf("匹配任务异常：%+v", matched)
	}
}

func TestEmbyMatchScorePrefersTMDBIDAndYear(t *testing.T) {
	year := 2024
	item := strmscrape.Item{Title: "测试电影", FolderName: "测试电影 (2024)", MediaType: strmscrape.MediaTypeMovie, TMDBID: "123", Year: &year}
	withTMDB := map[string]any{
		"Id": "emby-1", "Name": "完全不同的名称", "Type": "Movie", "ProductionYear": 2024,
		"ProviderIds": map[string]any{"Tmdb": "123"},
	}
	withoutTMDB := map[string]any{"Id": "emby-2", "Name": "测试电影", "Type": "Movie", "ProductionYear": 2024}
	if got := EmbyMatchScore(item, withTMDB); got <= EmbyMatchScore(item, withoutTMDB) {
		t.Fatalf("TMDB 精确匹配分数异常：with=%d without=%d", got, EmbyMatchScore(item, withoutTMDB))
	}
	wrongType := map[string]any{"Name": "测试电影", "Type": "Series", "ProductionYear": 2024}
	if got := EmbyMatchScore(item, wrongType); got != 0 {
		t.Fatalf("类型不一致不应匹配：%d", got)
	}
}

func TestTargetRelativePath(t *testing.T) {
	if got := TargetRelativePath("/云影视/电视剧", "/归档影视/剧集", []string{"钟表馆事件 (2026)"}); !reflect.DeepEqual(got, []string{"电视剧"}) {
		t.Fatalf("剧集归档目标相对路径=%v", got)
	}
	if got := TargetRelativePath("/云影视/电影", "/归档影视/电影", []string{"测试电影 (2026)"}); len(got) != 0 {
		t.Fatalf("电影归档不应重复电影目录：%v", got)
	}
	if got := TargetRelativePath("/云影视/电视剧", "/归档影视/剧集", []string{"分类", "剧集 (2026)"}); !reflect.DeepEqual(got, []string{"电视剧", "分类"}) {
		t.Fatalf("嵌套剧集归档目标相对路径=%v", got)
	}
}

func TestMatchStrmTasksSkipsPausedTasks(t *testing.T) {
	tasks := []*domain.StrmTask{
		{ID: 1, Name: "电影", Status: domain.StrmStatusActive},
		{ID: 2, Name: "电影-备用", Status: domain.StrmStatusPaused},
		{ID: 3, Name: "归档 · 剧集", Status: domain.StrmStatusPaused},
		{ID: 4, Name: "剧集", Status: domain.StrmStatusRunning},
	}
	if got := MatchStrmTasks(tasks, "电影"); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("已禁用的 STRM 任务不应参与匹配：%+v", got)
	}
	if got := MatchStrmTasks(tasks, "归档 · 剧集"); len(got) != 0 {
		t.Fatalf("归档媒体库不应回退到普通任务：%+v", got)
	}
}
