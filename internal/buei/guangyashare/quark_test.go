package guangyashare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

const (
	testQuarkShareID  = "0e1b2f7f3cd4"
	testQuarkPassword = "ab12"
)

func newTestQuarkService(t *testing.T, handler http.Handler) *Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Service{
		log: nil,
		providers: map[string]shareProvider{
			ProviderQuark: &quarkProvider{client: &quarkShareClient{
				http:    srv.Client(),
				baseURL: srv.URL,
			}},
		},
	}
}

// fakeQuarkShareAPI 模拟夸克分享页接口（token + detail）。
func fakeQuarkShareAPI(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathQuarkShareToken:
			var body struct {
				PwdID    string `json:"pwd_id"`
				Passcode string `json:"passcode"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.PwdID != testQuarkShareID {
				writeJSON(w, map[string]any{"status": 404, "code": 41004, "message": "分享不存在"})
				return
			}
			if body.Passcode != testQuarkPassword {
				writeJSON(w, map[string]any{"status": 400, "code": 41010, "message": "分享提取码错误"})
				return
			}
			writeJSON(w, map[string]any{"status": 200, "code": 0, "data": map[string]any{"stoken": "quark-token"}})
		case pathQuarkShareDetail:
			query := r.URL.Query()
			if query.Get("stoken") != "quark-token" {
				writeJSON(w, map[string]any{"status": 400, "code": 41010, "message": "分享提取码错误"})
				return
			}
			if query.Get("_page") != "1" {
				writeJSON(w, map[string]any{"status": 200, "code": 0, "data": map[string]any{"list": []any{}}})
				return
			}
			list := []map[string]any{
				{"fid": "d1", "file_name": "剧集", "dir": true, "share_fid_token": "tok-d1"},
				{"fid": "f1", "file_name": "01.mp4", "dir": false, "size": 1024, "updated_at": 1700000000000, "share_fid_token": "tok-f1"},
			}
			if query.Get("pdir_fid") == "d1" {
				list = []map[string]any{
					{"fid": "f2", "file_name": "02.mkv", "dir": false, "size": 2048, "updated_at": 1700000001000, "share_fid_token": "tok-f2"},
				}
			}
			writeJSON(w, map[string]any{"status": 200, "code": 0, "data": map[string]any{
				"list":  list,
				"share": map[string]any{"title": "夸克测试分享"},
			}})
		default:
			http.NotFound(w, r)
		}
	})
}

func TestQuarkParseNeedsPassword(t *testing.T) {
	svc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	res, err := svc.Parse(context.Background(), ParseInput{
		URL: "https://pan.quark.cn/s/" + testQuarkShareID,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !res.NeedPassword || res.ShareID != testQuarkShareID || res.Provider != ProviderQuark {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestQuarkParseListsRoot(t *testing.T) {
	svc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	res, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://pan.quark.cn/s/" + testQuarkShareID,
		Password: testQuarkPassword,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if res.NeedPassword {
		t.Fatalf("unexpected need_password")
	}
	if res.Provider != ProviderQuark || res.ProviderName != "夸克网盘" {
		t.Fatalf("unexpected provider: %+v", res)
	}
	if res.Title != "夸克测试分享" {
		t.Fatalf("Title = %q", res.Title)
	}
	if len(res.Items) != 2 {
		t.Fatalf("len(items) = %d", len(res.Items))
	}
	file := res.Items[1]
	if file.ID != "f1" || file.IsDir || file.Token != "tok-f1" || file.Kind != KindVideo {
		t.Fatalf("unexpected item: %+v", file)
	}
	if !res.Items[0].IsDir || res.Items[0].Kind != KindFolder {
		t.Fatalf("unexpected folder item: %+v", res.Items[0])
	}
}

func TestQuarkParseSavesShareTitle(t *testing.T) {
	svc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	repo := newFakeLinkRepo()
	svc.links = repo
	if _, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://pan.quark.cn/s/" + testQuarkShareID,
		Password: testQuarkPassword,
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	saved := repo.links[testQuarkShareID]
	if saved == nil {
		t.Fatal("share was not saved")
	}
	if saved.Title != "夸克测试分享" {
		t.Fatalf("saved title = %q, want 夸克测试分享", saved.Title)
	}
	if saved.Provider != ProviderQuark {
		t.Fatalf("saved provider = %q, want quark", saved.Provider)
	}
}

func TestQuarkListSubFolder(t *testing.T) {
	svc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	_, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://pan.quark.cn/s/" + testQuarkShareID,
		Password: testQuarkPassword,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	res, err := svc.List(context.Background(), ListInput{
		Provider:   ProviderQuark,
		ShareID:    testQuarkShareID,
		Password:   testQuarkPassword,
		ParentID:   "d1",
		ParentPath: "/剧集",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Token != "tok-f2" {
		t.Fatalf("unexpected items: %+v", res.Items)
	}
	if res.Items[0].Path != "/剧集/02.mkv" {
		t.Fatalf("unexpected path: %q", res.Items[0].Path)
	}
}

func TestQuarkParseWrongPassword(t *testing.T) {
	svc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	_, err := svc.Parse(context.Background(), ParseInput{
		URL:      "https://pan.quark.cn/s/" + testQuarkShareID,
		Password: "bad1",
	})
	if err == nil || !strings.Contains(err.Error(), "提取码") {
		t.Fatalf("expected passcode error, got %v", err)
	}
}

func TestQuarkParseMissingShare(t *testing.T) {
	svc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	_, err := svc.Parse(context.Background(), ParseInput{
		URL: "https://pan.quark.cn/s/ffffffffffff",
	})
	if err == nil || !strings.Contains(err.Error(), "分享不存在") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

// fakeQuarkRestoreDriver 记录驱动收到的转存条目。
type fakeQuarkRestoreDriver struct {
	*fakeRenameDriver
	req      driver.ShareRestoreRequest
	restored bool
}

func (d *fakeQuarkRestoreDriver) RestoreShare(_ context.Context, req driver.ShareRestoreRequest) (string, bool, error) {
	d.req = req
	if !d.restored {
		d.items = append(d.items, domain.FileItem{ID: "t1", Name: "01.mp4"})
		d.restored = true
	}
	return "task-quark", true, nil
}

var _ driver.ShareRestorer = (*fakeQuarkRestoreDriver)(nil)

func TestQuarkTransferPassesTokens(t *testing.T) {
	shareSvc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	drv := &fakeQuarkRestoreDriver{fakeRenameDriver: &fakeRenameDriver{renamed: map[string]string{}, failOn: map[string]bool{}}}
	svc := &Service{
		providers: shareSvc.providers,
		exec:      driverexec.New(&fakeRenameProvider{drv: drv}, nil),
		accounts:  &fakeAccountRepo{account: &domain.Account{ID: 5, DriverType: "quark", IsActive: true}},
		log:       nil,
	}
	res, err := svc.Transfer(context.Background(), TransferInput{
		URL:       "https://pan.quark.cn/s/" + testQuarkShareID,
		Password:  testQuarkPassword,
		AccountID: 5,
		ParentID:  "root",
		FileIDs:   []string{"f1"},
		Files: []TransferFile{
			{FileID: "f1", Token: "tok-f1", OriginalName: "01.mp4", NewName: "01.mp4"},
		},
	})
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if !res.Completed || res.TaskID != "task-quark" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if drv.req.Provider != ProviderQuark || drv.req.ShareID != testQuarkShareID || drv.req.AccessToken != "quark-token" {
		t.Fatalf("unexpected driver request: %+v", drv.req)
	}
	if len(drv.req.Entries) != 1 || drv.req.Entries[0].ID != "f1" || drv.req.Entries[0].Token != "tok-f1" {
		t.Fatalf("unexpected entries: %+v", drv.req.Entries)
	}
	if drv.req.ParentID != "root" {
		t.Fatalf("unexpected parent: %q", drv.req.ParentID)
	}
}

func TestTransferRejectsCrossProviderAccount(t *testing.T) {
	shareSvc := newTestQuarkService(t, fakeQuarkShareAPI(t))
	drv := &fakeQuarkRestoreDriver{fakeRenameDriver: &fakeRenameDriver{renamed: map[string]string{}, failOn: map[string]bool{}}}
	svc := &Service{
		providers: shareSvc.providers,
		exec:      driverexec.New(&fakeRenameProvider{drv: drv}, nil),
		accounts:  &fakeAccountRepo{account: &domain.Account{ID: 2, DriverType: "guangya", IsActive: true}},
	}
	_, err := svc.Transfer(context.Background(), TransferInput{
		URL:       "https://pan.quark.cn/s/" + testQuarkShareID,
		Password:  testQuarkPassword,
		AccountID: 2,
		FileIDs:   []string{"f1"},
	})
	if err == nil || !strings.Contains(err.Error(), "只能转存到夸克网盘账号") {
		t.Fatalf("expected provider mismatch error, got %v", err)
	}
}

func TestParseShareLinkDetectsProvider(t *testing.T) {
	tests := []struct {
		input        string
		wantProvider string
		wantID       string
	}{
		{"https://pan.quark.cn/s/0e1b2f7f3cd4", ProviderQuark, "0e1b2f7f3cd4"},
		{"我用夸克网盘分享了「剧集」，链接：https://pan.quark.cn/s/0e1b2f7f3cd4 提取码：ab12", ProviderQuark, "0e1b2f7f3cd4"},
		{"https://www.guangyapan.com/s/1969341399784386635_al18HJA7s7gOUCjJ", ProviderGuangya, "1969341399784386635_al18HJA7s7gOUCjJ"},
		{"活动页 https://example.com/abc 分享 https://pan.quark.cn/s/0e1b2f7f3cd4", ProviderQuark, "0e1b2f7f3cd4"},
	}
	for _, tc := range tests {
		info, err := ParseShareLink(tc.input)
		if err != nil {
			t.Fatalf("ParseShareLink(%q) error = %v", tc.input, err)
		}
		if info.Provider != tc.wantProvider || info.ShareID != tc.wantID {
			t.Fatalf("ParseShareLink(%q) = %+v", tc.input, info)
		}
	}
	if info, err := ParseShareLink("https://pan.quark.cn/s/0e1b2f7f3cd4?pwd=ab12"); err != nil || info.Password != "ab12" {
		t.Fatalf("query passcode not parsed: %+v, %v", info, err)
	}
	if _, err := ParseShareLink("https://example.com/s/abcdefgh"); err == nil {
		t.Fatal("expected error for unsupported host")
	}
}
