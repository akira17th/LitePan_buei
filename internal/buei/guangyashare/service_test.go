package guangyashare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	links  map[string]*domain.GuangyaShareLink
	nextID int64
}

func newFakeLinkRepo() *fakeLinkRepo {
	return &fakeLinkRepo{links: make(map[string]*domain.GuangyaShareLink)}
}

func (r *fakeLinkRepo) List(context.Context) ([]*domain.GuangyaShareLink, error) {
	out := make([]*domain.GuangyaShareLink, 0, len(r.links))
	for _, link := range r.links {
		copy := *link
		out = append(out, &copy)
	}
	return out, nil
}

func (r *fakeLinkRepo) Upsert(_ context.Context, link *domain.GuangyaShareLink) (*domain.GuangyaShareLink, error) {
	existing := r.links[link.ShareID]
	if existing == nil {
		r.nextID++
		existing = &domain.GuangyaShareLink{ID: r.nextID, ShareID: link.ShareID, CreatedAt: link.CreatedAt}
		r.links[link.ShareID] = existing
	}
	existing.URL = link.URL
	existing.Password = link.Password
	existing.Title = link.Title
	existing.Owner = link.Owner
	existing.UpdatedAt = link.UpdatedAt
	existing.LastUsedAt = link.LastUsedAt
	copy := *existing
	return &copy, nil
}

func (r *fakeLinkRepo) Delete(_ context.Context, id int64) error {
	for shareID, link := range r.links {
		if link.ID == id {
			delete(r.links, shareID)
			return nil
		}
	}
	return domain.Errf(domain.CodeNotFound)
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
