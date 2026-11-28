package guangyashare

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"litepan/internal/domain"
)

const (
	testShareID  = "1969341399784386635_al18HJA7s7gOUCjJ"
	testPassword = "kytt"
)

func newTestService(t *testing.T, handler http.Handler) *Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Service{
		log: nil,
		client: &shareClient{
			http:    srv.Client(),
			baseURL: srv.URL,
			did:     "test-device",
		},
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func fakeShareAPI(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case pathShareSummary:
			if body["shareId"] != testShareID {
				writeJSON(w, map[string]any{"code": 201, "msg": "invalid share"})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"shareId": testShareID, "title": "测试分享", "nickName": "光鸭用户", "needCode": true,
			}})
		case pathShareAccessToken:
			if body["code"] != testPassword {
				writeJSON(w, map[string]any{"code": 209, "msg": "提取码错误"})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{"accessToken": "share-token"}})
		case pathShareFileList:
			if body["accessToken"] != "share-token" {
				writeJSON(w, map[string]any{"code": 210, "msg": "分享accessToken无效"})
				return
			}
			switch body["parentId"] {
			case "":
				writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
					"total": 2,
					"list": []map[string]any{
						{"fileId": "d1", "parentId": "", "fileName": "剧集", "resType": 2, "utime": 1700000000, "fileType": 0},
						{"fileId": "f1", "parentId": "", "fileName": "readme.txt", "resType": 1, "fileSize": 12, "utime": 1700000001, "fileType": fileTypeDocument},
					},
				}})
			case "d1":
				writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
					"total": 1,
					"list": []map[string]any{
						{"fileId": "f2", "parentId": "d1", "fileName": "01.mp4", "resType": 1, "fileSize": 1024, "utime": 1700000002, "fileType": fileTypeVideo},
					},
				}})
			default:
				writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{}})
			}
		default:
			http.NotFound(w, r)
		}
	})
}

func TestServiceParse(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	res, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if res.ShareID != testShareID || res.Title != "测试分享" || res.Owner != "光鸭用户" {
		t.Fatalf("unexpected result meta: %+v", res)
	}
	if res.NeedPassword {
		t.Fatal("NeedPassword should be false when code is provided")
	}
	if len(res.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2: %+v", len(res.Items), res.Items)
	}
	want := map[string]string{
		"d1": "/剧集",
		"f1": "/readme.txt",
	}
	wantKind := map[string]string{
		"d1": KindFolder,
		"f1": KindDocument,
	}
	for _, item := range res.Items {
		if item.Path != want[item.ID] {
			t.Errorf("item %s path = %q, want %q", item.ID, item.Path, want[item.ID])
		}
		if item.Kind != wantKind[item.ID] {
			t.Errorf("item %s kind = %q, want %q", item.ID, item.Kind, wantKind[item.ID])
		}
	}
}

func TestServiceAllowsDuplicateLinks(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	repo := newFakeLinkRepo()
	svc.links = repo

	first, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("first Parse() error = %v", err)
	}
	second, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("second Parse() error = %v", err)
	}
	if first.SavedLinkID <= 0 || second.SavedLinkID <= 0 || first.SavedLinkID == second.SavedLinkID {
		t.Fatalf("duplicate parses should create distinct links: first=%d second=%d", first.SavedLinkID, second.SavedLinkID)
	}
	saved, err := svc.SavedLinks(context.Background())
	if err != nil || len(saved) != 2 {
		t.Fatalf("duplicate links not saved: %+v, %v", saved, err)
	}

	// 修改其中一条为另一条相同的分享，不应再触发重复校验。
	if _, err := svc.UpdateSavedLink(context.Background(), UpdateSavedInput{
		ID:       second.SavedLinkID,
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: testPassword,
	}); err != nil {
		t.Fatalf("UpdateSavedLink() should allow duplicate share, got %v", err)
	}
}

func TestServiceListFolder(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	res, err := svc.List(context.Background(), ListInput{
		URL:        "https://www.guangyapan.com/s/" + testShareID,
		Password:   testPassword,
		ParentID:   "d1",
		ParentPath: "/剧集",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1: %+v", len(res.Items), res.Items)
	}
	item := res.Items[0]
	if item.ID != "f2" || item.ParentID != "d1" || item.Path != "/剧集/01.mp4" || item.Depth != 1 {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.Kind != KindVideo {
		t.Fatalf("Kind = %q, want %q", item.Kind, KindVideo)
	}
}

func TestServiceParseNeedPassword(t *testing.T) {
	svc := newTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != pathShareSummary {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
			"shareId": testShareID, "title": "加密分享", "needCode": true,
		}})
	}))

	res, err := svc.Parse(context.Background(), ParseInput{URL: "https://www.guangyapan.com/s/" + testShareID})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !res.NeedPassword {
		t.Fatal("NeedPassword should be true")
	}
	if len(res.Items) != 0 {
		t.Fatalf("Items should be empty before password is provided, got %d", len(res.Items))
	}
}

func TestServiceParseNoCodeIgnoresPassword(t *testing.T) {
	var gotCode any = "unset"
	svc := newTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case pathShareSummary:
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"shareId": testShareID, "title": "公开分享", "needCode": false,
			}})
		case pathShareAccessToken:
			gotCode = body["code"]
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{"accessToken": "share-token"}})
		case pathShareFileList:
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	if _, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: "stale",
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if gotCode != "" {
		t.Fatalf("access token should be requested without code, got %#v", gotCode)
	}
}

func TestServiceParseWrongPassword(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	_, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: "bad",
	})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	if !strings.Contains(err.Error(), "提取码错误") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceParseInvalidShare(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	_, err := svc.Parse(context.Background(), ParseInput{
		URL: "https://www.guangyapan.com/s/1999999999999999999_xxxxxxxx",
	})
	if err == nil {
		t.Fatal("expected error for invalid share")
	}
	if !strings.Contains(err.Error(), "已失效") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeFileIDs(t *testing.T) {
	got := normalizeFileIDs([]string{" a ", "", "b", "a", "  "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("normalizeFileIDs() = %#v", got)
	}
}

type fakeLinkRepo struct {
	links  map[int64]*domain.GuangyaShareLink
	nextID int64
}

func newFakeLinkRepo() *fakeLinkRepo {
	return &fakeLinkRepo{links: make(map[int64]*domain.GuangyaShareLink)}
}

func (r *fakeLinkRepo) List(context.Context) ([]*domain.GuangyaShareLink, error) {
	out := make([]*domain.GuangyaShareLink, 0, len(r.links))
	for _, link := range r.links {
		copy := *link
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *fakeLinkRepo) Get(_ context.Context, id int64) (*domain.GuangyaShareLink, error) {
	link := r.links[id]
	if link == nil {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	copy := *link
	return &copy, nil
}

func (r *fakeLinkRepo) Create(_ context.Context, link *domain.GuangyaShareLink) (*domain.GuangyaShareLink, error) {
	r.nextID++
	copy := *link
	copy.ID = r.nextID
	r.links[copy.ID] = &copy
	result := copy
	return &result, nil
}

func (r *fakeLinkRepo) Touch(_ context.Context, id int64) error {
	link := r.links[id]
	if link == nil {
		return domain.Errf(domain.CodeNotFound)
	}
	link.LastUsedAt = time.Now().UTC()
	return nil
}

func (r *fakeLinkRepo) UpdateIdentity(_ context.Context, id int64, shareID, url, password, title, owner string, clearBrowsePath bool) (*domain.GuangyaShareLink, error) {
	link := r.links[id]
	if link == nil {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	link.ShareID = shareID
	link.URL = url
	link.Password = password
	link.Title = title
	link.Owner = owner
	link.UpdatedAt = time.Now().UTC()
	link.LastUsedAt = link.UpdatedAt
	if clearBrowsePath {
		link.LastBrowsePath = nil
	}
	copy := *link
	return &copy, nil
}

func (r *fakeLinkRepo) UpdateDestination(_ context.Context, id int64, dest *domain.GuangyaShareDestination) error {
	link := r.links[id]
	if link == nil {
		return domain.Errf(domain.CodeNotFound)
	}
	if dest == nil {
		link.LastDestination = nil
		return nil
	}
	copy := *dest
	link.LastDestination = &copy
	return nil
}

func (r *fakeLinkRepo) UpdateBrowsePath(_ context.Context, id int64, crumbs []domain.GuangyaShareBrowseCrumb) error {
	link := r.links[id]
	if link == nil {
		return domain.Errf(domain.CodeNotFound)
	}
	link.LastBrowsePath = append([]domain.GuangyaShareBrowseCrumb(nil), crumbs...)
	return nil
}

func (r *fakeLinkRepo) UpdateRenameReplace(_ context.Context, id int64, rule domain.GuangyaShareRenameReplace) error {
	link := r.links[id]
	if link == nil {
		return domain.Errf(domain.CodeNotFound)
	}
	link.RenameReplace = rule
	return nil
}

func (r *fakeLinkRepo) UpdateSchedule(_ context.Context, id int64, schedule domain.IngestUpdateSchedule) error {
	link := r.links[id]
	if link == nil {
		return domain.Errf(domain.CodeNotFound)
	}
	normalized, err := schedule.Normalize()
	if err != nil {
		return err
	}
	link.UpdateSchedule = normalized
	return nil
}

func (r *fakeLinkRepo) Delete(_ context.Context, id int64) error {
	if r.links[id] == nil {
		return domain.Errf(domain.CodeNotFound)
	}
	delete(r.links, id)
	return nil
}

type fakeTransferredRepo struct {
	files map[int64][]*domain.GuangyaShareTransferredFile
}

func newFakeTransferredRepo() *fakeTransferredRepo {
	return &fakeTransferredRepo{files: make(map[int64][]*domain.GuangyaShareTransferredFile)}
}

func (r *fakeTransferredRepo) ListByLink(_ context.Context, linkID int64) ([]*domain.GuangyaShareTransferredFile, error) {
	return append([]*domain.GuangyaShareTransferredFile(nil), r.files[linkID]...), nil
}

func (r *fakeTransferredRepo) UpsertMany(_ context.Context, linkID int64, files []*domain.GuangyaShareTransferredFile) error {
	r.files[linkID] = append([]*domain.GuangyaShareTransferredFile(nil), files...)
	return nil
}

func (r *fakeTransferredRepo) DeleteByLink(_ context.Context, linkID int64) error {
	delete(r.files, linkID)
	return nil
}

type fakeConfigRepo struct {
	values map[string]string
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{values: make(map[string]string)}
}

func (r *fakeConfigRepo) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := r.values[key]
	return value, ok, nil
}

func (r *fakeConfigRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *fakeConfigRepo) All(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func TestServiceParseReturnsSavedDestinationAndLastBase(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	repo := newFakeLinkRepo()
	repo.links[1] = &domain.GuangyaShareLink{
		ID:      1,
		ShareID: testShareID,
		LastDestination: &domain.GuangyaShareDestination{
			AccountID: 2,
			ParentID:  "dir-1",
			BasePath:  "/媒体/电视剧",
			BaseDirs:  []string{"媒体", "电视剧"},
			Title:     "走马上任 (2026)",
			Season:    2,
			IsSeries:  true,
		},
		LastBrowsePath: []domain.GuangyaShareBrowseCrumb{
			{ID: "", Name: "全部文件", Path: ""},
			{ID: "d1", Name: "剧集", Path: "/剧集"},
		},
		RenameReplace: domain.GuangyaShareRenameReplace{
			Enabled: true,
			Find:    "\\s+",
			Rules: []domain.GuangyaShareRenameRule{
				{Find: "\\s+", ReplaceWith: " "},
				{Find: "2160p", ReplaceWith: ""},
			},
		},
	}
	svc.links = repo
	configs := newFakeConfigRepo()
	svc.configs = configs
	rawBase, _ := json.Marshal(domain.GuangyaShareDestinationBase{
		AccountID: 3,
		ParentID:  "root-3",
		Path:      "/最近目录",
		Dirs:      []string{"最近目录"},
	})
	configs.values[lastBaseConfigKey] = string(rawBase)

	res, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: testPassword,
		SavedID:  1,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if res.LastDestination == nil || res.LastDestination.Title != "走马上任 (2026)" || res.LastDestination.Season != 2 {
		t.Fatalf("unexpected last destination: %+v", res.LastDestination)
	}
	if res.LastBase == nil || res.LastBase.AccountID != 3 || res.LastBase.Path != "/最近目录" ||
		len(res.LastBase.Dirs) != 1 || res.LastBase.Dirs[0] != "最近目录" {
		t.Fatalf("unexpected last base: %+v", res.LastBase)
	}
	if len(res.LastBrowsePath) != 2 || res.LastBrowsePath[1].ID != "d1" {
		t.Fatalf("unexpected last browse path: %+v", res.LastBrowsePath)
	}
	if res.RenameReplace == nil || !res.RenameReplace.Enabled || res.RenameReplace.Find != "\\s+" {
		t.Fatalf("unexpected rename replace rule: %+v", res.RenameReplace)
	}
	if rules := res.RenameReplace.NormalizeRules(); len(rules) != 2 || rules[1].ReplaceWith != "" {
		t.Fatalf("multi rename rules not returned: %+v", res.RenameReplace)
	}
}

func TestScrapeEnabled(t *testing.T) {
	base := TransferInput{AutoStrm: true, AutoScrape: true, ScrapeTaskID: 3}
	if !scrapeEnabled(base) {
		t.Fatal("STRM 与刮削都打开且选了任务时应启用刮削")
	}
	for name, in := range map[string]TransferInput{
		"关闭 STRM": {AutoStrm: false, AutoScrape: true, ScrapeTaskID: 3},
		"关闭刮削":    {AutoStrm: true, AutoScrape: false, ScrapeTaskID: 3},
		"未选择任务":   {AutoStrm: true, AutoScrape: true},
	} {
		if scrapeEnabled(in) {
			t.Fatalf("%s 时不应刮削", name)
		}
	}
}

func TestServiceScrapeAfterTransfer(t *testing.T) {
	svc := &Service{}
	if started, errText := svc.scrapeAfterTransfer(context.Background(), 5, nil); started || errText != "" {
		t.Fatalf("未注入刮削器时应静默跳过，实际 = %v/%q", started, errText)
	}

	var calls []string
	svc.SetScrapeRunner(
		func(_ context.Context, strmTaskID int64) error {
			if strmTaskID != 7 {
				t.Fatalf("unexpected strm task: %d", strmTaskID)
			}
			calls = append(calls, "rebuild")
			return nil
		},
		func(_ context.Context, strmTaskID int64, remoteDirs []string) error {
			if strmTaskID != 7 {
				t.Fatalf("unexpected strm task: %d", strmTaskID)
			}
			if len(remoteDirs) != 2 || remoteDirs[0] != "国漫" || remoteDirs[1] != "剧名" {
				t.Fatalf("unexpected remote dirs: %v", remoteDirs)
			}
			calls = append(calls, "start")
			return nil
		},
		func(_ context.Context, strmTaskID int64) error {
			calls = append(calls, "wait")
			return nil
		},
	)
	started, errText := svc.scrapeAfterTransfer(context.Background(), 7, []string{"国漫", "剧名"})
	if !started || errText != "" {
		t.Fatalf("unexpected scrape result: %v/%q", started, errText)
	}
	if len(calls) != 2 || calls[0] != "rebuild" || calls[1] != "start" {
		t.Fatalf("应先重建索引再开始刮削，实际调用顺序 = %v", calls)
	}

	startCalls := 0
	svc.SetScrapeRunner(
		func(context.Context, int64) error { return errors.New("index failed") },
		func(context.Context, int64, []string) error { startCalls++; return nil },
		nil,
	)
	if started, errText := svc.scrapeAfterTransfer(context.Background(), 7, nil); started || errText == "" {
		t.Fatalf("重建索引失败时应回传错误，实际 = %v/%q", started, errText)
	}
	if startCalls != 0 {
		t.Fatal("重建索引失败后不应继续启动刮削")
	}
}

func TestEmbyActionNormalize(t *testing.T) {
	in := TransferInput{RefreshEmby: true, EmbyConfigID: " c1 ", EmbyLibraryID: " lib1 ", EmbyMode: " library "}
	action := in.embyAction()
	if !action.enabled || action.configID != "c1" || action.mode != "library" || action.libraryID != "lib1" {
		t.Fatalf("unexpected emby action: %+v", action)
	}
	if got := (TransferInput{EmbyMode: "other"}).embyAction().mode; got != "global" {
		t.Fatalf("未知范围应回退为 global，实际 %q", got)
	}
}

func TestServiceEmbyActions(t *testing.T) {
	svc := &Service{progress: newTransferProgressTracker()}
	if ok, errText := svc.refreshEmbyNow(context.Background(), embyAction{enabled: true}); ok || errText != "" {
		t.Fatalf("未注入刷库器时应静默跳过，实际 = %v/%q", ok, errText)
	}

	var calls []string
	svc.SetScrapeRunner(nil, nil, func(_ context.Context, strmTaskID int64) error {
		if strmTaskID != 11 {
			t.Fatalf("unexpected strm task: %d", strmTaskID)
		}
		calls = append(calls, "wait")
		return nil
	})
	svc.SetEmbyRefresher(func(_ context.Context, configID, mode, libraryID string) error {
		calls = append(calls, "refresh:"+configID+":"+mode+":"+libraryID)
		return nil
	})
	svc.progress.start("p1")
	svc.waitAndRefreshEmby("p1", 11, embyAction{enabled: true, configID: "c1", mode: "library", libraryID: "lib1"})
	if len(calls) != 2 || calls[0] != "wait" || calls[1] != "refresh:c1:library:lib1" {
		t.Fatalf("应先等刮削结束再刷库，实际调用顺序 = %v", calls)
	}
	if p, ok := svc.progress.get("p1"); !ok || p.Phase != GuangyaSharePhaseDone || p.Error != "" {
		t.Fatalf("刷库成功后进度应为 done，实际 %+v", p)
	}

	svc.progress.start("p2")
	svc.SetEmbyRefresher(func(context.Context, string, string, string) error {
		return errors.New("emby 不可用")
	})
	svc.waitAndRefreshEmby("p2", 11, embyAction{enabled: true, configID: "c1"})
	if p, ok := svc.progress.get("p2"); !ok || p.Phase != GuangyaSharePhaseFailed || p.Error == "" {
		t.Fatalf("刷库失败后进度应为 failed，实际 %+v", p)
	}

	if ok, errText := svc.refreshEmbyNow(context.Background(), embyAction{enabled: false, configID: "c1"}); ok || errText != "" {
		t.Fatalf("未开启刷库时不应调用，实际 = %v/%q", ok, errText)
	}
}

func TestServiceSaveDestination(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	repo := newFakeLinkRepo()
	repo.links[1] = &domain.GuangyaShareLink{ID: 1, ShareID: testShareID}
	svc.links = repo
	configs := newFakeConfigRepo()
	svc.configs = configs

	dest := &domain.GuangyaShareDestination{
		AccountID:     2,
		ParentID:      "dir-2",
		BasePath:      "/媒体/电影",
		BaseDirs:      []string{"媒体", "电影"},
		Title:         "测试电影 (2026)",
		Season:        3,
		IsSeries:      false,
		AutoStrm:      true,
		RefreshEmby:   true,
		EmbyConfigID:  " emby-1 ",
		EmbyMode:      " library ",
		EmbyLibraryID: " lib-1 ",
	}
	svc.saveDestination(context.Background(), 1, dest)

	saved := repo.links[1].LastDestination
	if saved == nil || saved.ParentID != "dir-2" || saved.Title != "测试电影 (2026)" || saved.Season != 3 || saved.IsSeries || !saved.AutoStrm ||
		!saved.RefreshEmby || saved.EmbyConfigID != "emby-1" || saved.EmbyMode != "library" ||
		saved.EmbyLibraryID != "lib-1" || len(saved.BaseDirs) != 2 || saved.BaseDirs[1] != "电影" {
		t.Fatalf("unexpected saved destination: %+v", saved)
	}
	raw, ok := configs.values[lastBaseConfigKey]
	if !ok {
		t.Fatal("last base was not saved")
	}
	var base domain.GuangyaShareDestinationBase
	if err := json.Unmarshal([]byte(raw), &base); err != nil {
		t.Fatalf("unmarshal last base: %v", err)
	}
	if base.AccountID != 2 || base.ParentID != "dir-2" || base.Path != "/媒体/电影" ||
		len(base.Dirs) != 2 || base.Dirs[0] != "媒体" {
		t.Fatalf("unexpected last base: %+v", base)
	}
}

func TestServiceDiscard(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	transfers := newFakeTransferredRepo()
	svc.transfers = transfers

	count, err := svc.Discard(context.Background(), DiscardInput{
		SavedID: 1,
		Files: []TransferFile{
			{FileID: "f1", OriginalName: "01.mp4"},
			{FileID: "f2", OriginalName: "02.mp4", NewName: "ignored.mp4"},
		},
	})
	if err != nil {
		t.Fatalf("Discard() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("Discard() count = %d, want 2", count)
	}
	files := transfers.files[1]
	if len(files) != 2 {
		t.Fatalf("unexpected discarded files: %+v", files)
	}
	for _, file := range files {
		if file.Status != domain.GuangyaShareFileDiscarded || file.NewName != file.OriginalName {
			t.Fatalf("unexpected discarded record: %+v", file)
		}
	}
}

func TestUpdateSavedLinkPreservesSettings(t *testing.T) {
	const newShareID = "1969341399784386636_newShareID"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case pathShareSummary:
			if body["shareId"] != newShareID {
				writeJSON(w, map[string]any{"code": 201, "msg": "invalid share"})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"shareId": newShareID, "title": "新测试分享", "nickName": "新用户", "needCode": true,
			}})
		case pathShareAccessToken:
			if body["code"] != testPassword {
				writeJSON(w, map[string]any{"code": 209, "msg": "提取码错误"})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{"accessToken": "new-token"}})
		case pathShareFileList:
			if body["accessToken"] != "new-token" {
				writeJSON(w, map[string]any{"code": 210, "msg": "分享accessToken无效"})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"total": 1,
				"list": []map[string]any{
					{"fileId": "new-f1", "parentId": "", "fileName": "new.mp4", "resType": 1, "fileSize": 100, "utime": 1700000000, "fileType": fileTypeVideo},
				},
			}})
		default:
			writeJSON(w, map[string]any{"code": 201, "msg": "unknown"})
		}
	})
	svc := newTestService(t, handler)
	oldDestination := &domain.GuangyaShareDestination{
		AccountID: 2, ParentID: "p1", BasePath: "/媒体", Title: "测试剧", Season: 1, IsSeries: true,
		AutoStrm: true, AutoScrape: true, RefreshEmby: true, EmbyConfigID: "emby-1",
	}
	oldRename := domain.GuangyaShareRenameReplace{
		Enabled: true,
		Rules:   []domain.GuangyaShareRenameRule{{Find: "old", ReplaceWith: "new"}},
	}
	repo := newFakeLinkRepo()
	repo.links[1] = &domain.GuangyaShareLink{
		ID: 1, ShareID: testShareID, URL: "https://www.guangyapan.com/s/" + testShareID,
		Title: "旧分享", Owner: "旧用户",
		LastDestination: oldDestination,
		LastBrowsePath:  []domain.GuangyaShareBrowseCrumb{{ID: "d1", Name: "目录", Path: "/目录"}},
		RenameReplace:   oldRename,
	}
	svc.links = repo
	transfers := newFakeTransferredRepo()
	transfers.files[1] = []*domain.GuangyaShareTransferredFile{{FileID: "old-f1"}}
	svc.transfers = transfers

	res, err := svc.UpdateSavedLink(context.Background(), UpdateSavedInput{
		ID:       1,
		URL:      "https://www.guangyapan.com/s/" + newShareID,
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("UpdateSavedLink() error = %v", err)
	}
	if res.ShareID != newShareID || res.Title != "新测试分享" || len(res.Items) != 1 {
		t.Fatalf("unexpected parse result: %+v", res)
	}
	if res.LastDestination == nil || res.LastDestination.Title != "测试剧" || !res.LastDestination.AutoStrm {
		t.Fatalf("last destination not preserved: %+v", res.LastDestination)
	}
	if res.RenameReplace == nil || !res.RenameReplace.Enabled || len(res.RenameReplace.NormalizeRules()) != 1 {
		t.Fatalf("rename rule not preserved: %+v", res.RenameReplace)
	}
	if len(res.LastBrowsePath) != 0 {
		t.Fatalf("browse path should be cleared, got %+v", res.LastBrowsePath)
	}
	saved := repo.links[1]
	if saved == nil || saved.ID != 1 || saved.Title != "新测试分享" || saved.URL == "" {
		t.Fatalf("saved link identity not updated: %+v", saved)
	}
	if saved.LastDestination == nil || saved.LastDestination.Title != "测试剧" {
		t.Fatalf("saved destination not preserved: %+v", saved.LastDestination)
	}
	if !saved.RenameReplace.Enabled {
		t.Fatalf("saved rename rule not preserved: %+v", saved.RenameReplace)
	}
	if len(saved.LastBrowsePath) != 0 {
		t.Fatalf("saved browse path should be cleared, got %+v", saved.LastBrowsePath)
	}
	if len(transfers.files[1]) != 0 {
		t.Fatalf("old transferred history should be cleared, got %+v", transfers.files[1])
	}
}

func TestServiceTransferredHistory(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	repo := newFakeLinkRepo()
	repo.links[1] = &domain.GuangyaShareLink{ID: 1, ShareID: testShareID}
	svc.links = repo
	transfers := newFakeTransferredRepo()
	transfers.files[1] = []*domain.GuangyaShareTransferredFile{
		{FileID: "f1", OriginalName: "01.mp4", NewName: "剧名-S01E01-01.mp4", AccountID: 2},
	}
	svc.transfers = transfers

	res, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: testPassword,
		SavedID:  1,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(res.TransferredFiles) != 1 || res.TransferredFiles[0].NewName != "剧名-S01E01-01.mp4" {
		t.Fatalf("unexpected transferred files: %+v", res.TransferredFiles)
	}

	if err := svc.DeleteSavedLink(context.Background(), 1); err != nil {
		t.Fatalf("DeleteSavedLink() error = %v", err)
	}
	if len(transfers.files[1]) != 0 {
		t.Fatalf("transferred history should be deleted with link, got %+v", transfers.files[1])
	}
}

func TestTargetDirectorySegments(t *testing.T) {
	got := targetDirectorySegments([]string{"媒体", "", "电视剧 "}, []string{"走马上任 (2026)", "Season 01"})
	want := []string{"媒体", "电视剧", "走马上任 (2026)", "Season 01"}
	if len(got) != len(want) {
		t.Fatalf("targetDirectorySegments() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targetDirectorySegments() = %v, want %v", got, want)
		}
	}
}

func TestServiceGenerateStrm(t *testing.T) {
	svc := &Service{}
	if generated, matched, errText := svc.generateStrm(context.Background(), 2, "dir-1", []string{"剧集"}); generated != 0 || matched || errText != "" {
		t.Fatalf("未注入生成器时应静默跳过，实际 = %d/%v/%q", generated, matched, errText)
	}

	var gotDirs []string
	svc.SetStrmGenerator(func(_ context.Context, accountID int64, parentID string, dirs []string) (int64, bool, error) {
		if accountID != 2 || parentID != "dir-1" {
			t.Fatalf("unexpected target: %d %s", accountID, parentID)
		}
		gotDirs = append([]string{}, dirs...)
		return 3, true, nil
	})
	generated, matched, errText := svc.generateStrm(context.Background(), 2, "dir-1", []string{"剧集", "Season 01"})
	if generated != 3 || !matched || errText != "" {
		t.Fatalf("unexpected generate result: %d/%v/%q", generated, matched, errText)
	}
	if len(gotDirs) != 2 || gotDirs[1] != "Season 01" {
		t.Fatalf("unexpected dirs: %v", gotDirs)
	}

	svc.SetStrmGenerator(func(context.Context, int64, string, []string) (int64, bool, error) {
		return 0, false, errors.New("strm task busy")
	})
	if generated, matched, errText := svc.generateStrm(context.Background(), 2, "dir-1", nil); generated != 0 || matched || errText == "" {
		t.Fatalf("生成失败时应回传错误信息，实际 = %d/%v/%q", generated, matched, errText)
	}
}

func TestNormalizeDestinationBasePath(t *testing.T) {
	tests := map[string]string{
		"":         "/",
		"/":        "/",
		"媒体/电视剧":   "/媒体/电视剧",
		"/媒体/电视剧/": "/媒体/电视剧",
	}
	for input, want := range tests {
		if got := normalizeDestinationBasePath(input); got != want {
			t.Fatalf("normalizeDestinationBasePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestServiceParseSavesAndDeletesLink(t *testing.T) {
	svc := newTestService(t, fakeShareAPI(t))
	repo := newFakeLinkRepo()
	svc.links = repo

	if _, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://www.guangyapan.com/s/" + testShareID,
		Password: testPassword,
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	saved, err := svc.SavedLinks(context.Background())
	if err != nil {
		t.Fatalf("SavedLinks() error = %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("len(saved) = %d, want 1", len(saved))
	}
	if saved[0].ShareID != testShareID || saved[0].Password != testPassword || saved[0].Title != "测试分享" {
		t.Fatalf("unexpected saved link: %+v", saved[0])
	}

	if err := svc.DeleteSavedLink(context.Background(), saved[0].ID); err != nil {
		t.Fatalf("DeleteSavedLink() error = %v", err)
	}
	saved, err = svc.SavedLinks(context.Background())
	if err != nil {
		t.Fatalf("SavedLinks() after delete error = %v", err)
	}
	if len(saved) != 0 {
		t.Fatalf("expected empty saved links, got %+v", saved)
	}
}

func TestServiceNeedPasswordDoesNotSave(t *testing.T) {
	svc := newTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathShareSummary {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
			"shareId": testShareID, "title": "加密分享", "needCode": true,
		}})
	}))
	repo := newFakeLinkRepo()
	svc.links = repo

	if _, err := svc.Parse(context.Background(), ParseInput{URL: "https://www.guangyapan.com/s/" + testShareID}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(repo.links) != 0 {
		t.Fatalf("need-password share should not be saved before code is verified, got %d", len(repo.links))
	}
}

// buei:ingest-update —— 收藏更新时间保存与清空。
func TestUpdateSavedSchedule(t *testing.T) {
	repo := newFakeLinkRepo()
	link, err := repo.Create(context.Background(), &domain.GuangyaShareLink{ShareID: "1969_sched", Title: "更新时间"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	svc := &Service{links: repo}

	want := domain.IngestUpdateSchedule{Weekday: 5, Hour: 19, Minute: 45}
	updated, err := svc.UpdateSavedSchedule(context.Background(), link.ID, want)
	if err != nil {
		t.Fatalf("UpdateSavedSchedule: %v", err)
	}
	if updated.UpdateSchedule != want {
		t.Fatalf("更新时间未保存：期望 %+v，实际 %+v", want, updated.UpdateSchedule)
	}

	cleared, err := svc.UpdateSavedSchedule(context.Background(), link.ID, domain.IngestUpdateSchedule{Weekday: domain.IngestUpdateNone})
	if err != nil {
		t.Fatalf("清空更新时间：%v", err)
	}
	if cleared.UpdateSchedule.IsSet() {
		t.Fatalf("更新时间应被清空：%+v", cleared.UpdateSchedule)
	}

	if _, err := svc.UpdateSavedSchedule(context.Background(), link.ID, domain.IngestUpdateSchedule{Weekday: 2, Hour: 25}); err == nil {
		t.Fatal("小时越界应报错")
	}
	if _, err := svc.UpdateSavedSchedule(context.Background(), 999, want); err == nil {
		t.Fatal("收藏不存在应报错")
	}
}
