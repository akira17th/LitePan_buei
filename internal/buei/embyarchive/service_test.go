package embyarchive

import (
	"context"
	"errors"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
)

type fakeRuleRepo struct {
	rules []*domain.BueiEmbyArchiveRule
}

func (f *fakeRuleRepo) List(context.Context) ([]*domain.BueiEmbyArchiveRule, error) {
	return f.rules, nil
}

func (f *fakeRuleRepo) Get(_ context.Context, archiveLibraryID string) (*domain.BueiEmbyArchiveRule, error) {
	for _, rule := range f.rules {
		if rule.ArchiveLibraryID == archiveLibraryID {
			return rule, nil
		}
	}
	return nil, domain.Errf(domain.CodeNotFound)
}

func (f *fakeRuleRepo) Save(_ context.Context, rule *domain.BueiEmbyArchiveRule) (*domain.BueiEmbyArchiveRule, error) {
	for i, existing := range f.rules {
		if existing.ArchiveLibraryID == rule.ArchiveLibraryID {
			f.rules[i] = rule
			return rule, nil
		}
	}
	f.rules = append(f.rules, rule)
	return rule, nil
}

func (f *fakeRuleRepo) UpdateSchedule(context.Context, string, time.Time, time.Time) error {
	return nil
}

type fakeEmby struct {
	libraries []embyproxy.Library
}

func (f *fakeEmby) ListLibraries(context.Context, ...string) ([]embyproxy.Library, error) {
	return f.libraries, nil
}

func (f *fakeEmby) RefreshLibrary(context.Context, embyproxy.RefreshRequest) (embyproxy.RefreshResult, error) {
	return embyproxy.RefreshResult{}, nil
}

func (f *fakeEmby) ListLibraryItemsRaw(context.Context, string, string) ([]map[string]any, error) {
	return nil, nil
}

func (f *fakeEmby) ListLibraryItemUserStatuses(context.Context, string, []map[string]any) (map[string][]embyproxy.LibraryUserItemStatus, error) {
	return nil, nil
}

func newRuleTestService(repo *fakeRuleRepo, emby *fakeEmby) *Service {
	return New(Options{Rules: repo, Emby: emby})
}

func TestSaveRuleRejectsClaimedSourceLibrary(t *testing.T) {
	repo := &fakeRuleRepo{rules: []*domain.BueiEmbyArchiveRule{{
		ArchiveLibraryID:   "archive-movie",
		ArchiveLibraryName: "归档 · 电影",
		SourceLibraries:    []domain.BueiEmbyArchiveSourceLibrary{{ID: "lib-movie", Name: "电影"}},
	}}}
	emby := &fakeEmby{libraries: []embyproxy.Library{
		{ID: "lib-movie", Name: "电影", CollectionType: "movies"},
		{ID: "archive-movie", Name: "归档 · 电影", CollectionType: "movies"},
		{ID: "archive-tv", Name: "归档 · 剧集", CollectionType: "tvshows"},
	}}
	svc := newRuleTestService(repo, emby)

	_, err := svc.SaveRule(context.Background(), SaveRuleInput{
		ArchiveLibraryID: "archive-tv",
		SourceLibraryIDs: []string{"lib-movie"},
		IntervalHours:    1,
	})
	if err == nil {
		t.Fatal("已被其他归档媒体库占用的来源应拒绝保存")
	}
	appErr, ok := domain.AsAppError(err)
	if !ok || appErr.Code != domain.CodeValidation {
		t.Fatalf("错误码异常：%v", err)
	}
}

func TestSaveRuleRequiresSourceLibrary(t *testing.T) {
	emby := &fakeEmby{libraries: []embyproxy.Library{{ID: "archive-tv", Name: "归档 · 剧集", CollectionType: "tvshows"}}}
	svc := newRuleTestService(&fakeRuleRepo{}, emby)
	if _, err := svc.SaveRule(context.Background(), SaveRuleInput{ArchiveLibraryID: "archive-tv", IntervalHours: 1}); err == nil {
		t.Fatal("没有来源媒体库时应拒绝保存")
	}
}

func TestSaveRuleNormalizesIntervalAndPreservesLastRun(t *testing.T) {
	lastRun := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	repo := &fakeRuleRepo{rules: []*domain.BueiEmbyArchiveRule{{
		ArchiveLibraryID:   "archive-tv",
		ArchiveLibraryName: "归档 · 剧集",
		SourceLibraries:    []domain.BueiEmbyArchiveSourceLibrary{{ID: "lib-tv", Name: "剧集"}},
		IntervalHours:      1,
		LastRunAt:          lastRun,
	}}}
	emby := &fakeEmby{libraries: []embyproxy.Library{
		{ID: "lib-tv", Name: "剧集", CollectionType: "tvshows"},
		{ID: "archive-tv", Name: "归档 · 剧集", CollectionType: "tvshows"},
	}}
	svc := newRuleTestService(repo, emby)

	saved, err := svc.SaveRule(context.Background(), SaveRuleInput{
		ArchiveLibraryID: "archive-tv",
		SourceLibraryIDs: []string{"lib-tv", "lib-tv"},
		IntervalHours:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.IntervalHours != 1 {
		t.Fatalf("非法频率应回落为 1 小时：%d", saved.IntervalHours)
	}
	if len(saved.SourceLibraries) != 1 {
		t.Fatalf("来源媒体库应去重：%+v", saved.SourceLibraries)
	}
	if !saved.LastRunAt.Equal(lastRun) {
		t.Fatalf("应保留上次执行时间：%v", saved.LastRunAt)
	}
}

func TestRuleViewMarksClaimedSources(t *testing.T) {
	repo := &fakeRuleRepo{rules: []*domain.BueiEmbyArchiveRule{{
		ArchiveLibraryID:   "archive-movie",
		ArchiveLibraryName: "归档 · 电影",
		SourceLibraries:    []domain.BueiEmbyArchiveSourceLibrary{{ID: "lib-movie", Name: "电影"}},
	}}}
	emby := &fakeEmby{libraries: []embyproxy.Library{
		{ID: "lib-movie", Name: "电影", CollectionType: "movies"},
		{ID: "lib-tv", Name: "剧集", CollectionType: "tvshows"},
		{ID: "archive-movie", Name: "归档 · 电影", CollectionType: "movies"},
		{ID: "archive-tv", Name: "归档 · 剧集", CollectionType: "tvshows"},
	}}
	svc := newRuleTestService(repo, emby)

	view, err := svc.RuleView(context.Background(), "", "archive-tv")
	if err != nil {
		t.Fatal(err)
	}
	if view.Rule.IntervalHours != 1 {
		t.Fatalf("未配置时频率应默认 1 小时：%d", view.Rule.IntervalHours)
	}
	if len(view.SourceLibraries) != 2 {
		t.Fatalf("来源选项不应包含归档媒体库：%+v", view.SourceLibraries)
	}
	for _, option := range view.SourceLibraries {
		switch option.ID {
		case "lib-movie":
			if option.ClaimedByLibraryName != "归档 · 电影" {
				t.Fatalf("已占用来源应标记占用方：%+v", option)
			}
		case "lib-tv":
			if option.ClaimedByLibraryID != "" {
				t.Fatalf("未占用来源不应标记占用方：%+v", option)
			}
		default:
			t.Fatalf("意外的来源选项：%+v", option)
		}
	}
}

func TestIsNotFound(t *testing.T) {
	if !isNotFound(domain.Errf(domain.CodeNotFound)) {
		t.Fatal("NOT_FOUND 应被识别")
	}
	if isNotFound(errors.New("boom")) {
		t.Fatal("普通错误不应被识别为 NOT_FOUND")
	}
}
