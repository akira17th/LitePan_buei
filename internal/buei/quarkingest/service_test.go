package quarkingest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"litepan/internal/domain"
)

type fakeAccountReader struct {
	account *domain.Account
	err     error
}

func (f *fakeAccountReader) Get(context.Context, int64) (*domain.Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.account, nil
}

type fakeFavoriteRepo struct {
	favorites map[int64]*domain.QuarkIngestFavorite
	renamed   map[int64]string
}

func (f *fakeFavoriteRepo) ListByAccount(_ context.Context, accountID int64) ([]*domain.QuarkIngestFavorite, error) {
	out := make([]*domain.QuarkIngestFavorite, 0, len(f.favorites))
	for _, favorite := range f.favorites {
		if favorite.AccountID == accountID {
			out = append(out, favorite)
		}
	}
	return out, nil
}

func (f *fakeFavoriteRepo) Create(_ context.Context, favorite *domain.QuarkIngestFavorite) (*domain.QuarkIngestFavorite, error) {
	if f.favorites == nil {
		f.favorites = map[int64]*domain.QuarkIngestFavorite{}
	}
	favorite.ID = int64(len(f.favorites) + 1)
	f.favorites[favorite.ID] = favorite
	return favorite, nil
}

func (f *fakeFavoriteRepo) Update(_ context.Context, favorite *domain.QuarkIngestFavorite) (*domain.QuarkIngestFavorite, error) {
	f.favorites[favorite.ID] = favorite
	return favorite, nil
}

func (f *fakeFavoriteRepo) Delete(_ context.Context, id, accountID int64) error {
	delete(f.favorites, id)
	return nil
}

func (f *fakeFavoriteRepo) UpdateFolderName(_ context.Context, id int64, name string) error {
	if f.renamed == nil {
		f.renamed = map[int64]string{}
	}
	f.renamed[id] = name
	return nil
}

type fakeConfigStore struct {
	values map[string]string
}

func (f *fakeConfigStore) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := f.values[key]
	return value, ok, nil
}

func (f *fakeConfigStore) Set(_ context.Context, key, value string) error {
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[key] = value
	return nil
}

type fakeFileOps struct {
	items      map[string][]domain.FileItem
	created    map[string]string
	nextID     int
	moves      []fakeMove
	renames    []fakeRename
	deletes    []fakeDelete
	moveErr    error
	deleteErr  error
	renameErrs map[string]error
}

type fakeMove struct {
	fileIDs        []string
	targetParentID string
	sourceParentID string
}

type fakeRename struct {
	fileID  string
	newName string
	parent  string
}

type fakeDelete struct {
	fileIDs []string
	parent  string
}

func newFakeFileOps() *fakeFileOps {
	return &fakeFileOps{
		items:      map[string][]domain.FileItem{},
		created:    map[string]string{},
		renameErrs: map[string]error{},
	}
}

func (f *fakeFileOps) List(_ context.Context, _ int64, parentID string, _ bool) ([]domain.FileItem, error) {
	return f.items[parentID], nil
}

func (f *fakeFileOps) CreateFolder(_ context.Context, _ int64, parentID, name string) (*domain.FileItem, error) {
	f.nextID++
	id := "folder-" + name
	f.created[parentID+"/"+name] = id
	f.items[parentID] = append(f.items[parentID], domain.FileItem{ID: id, Name: name, IsDir: true})
	return &domain.FileItem{ID: id, Name: name, IsDir: true}, nil
}

func (f *fakeFileOps) MoveFiles(_ context.Context, _ int64, fileIDs []string, targetParentID, sourceParentID string) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	f.moves = append(f.moves, fakeMove{fileIDs: append([]string(nil), fileIDs...), targetParentID: targetParentID, sourceParentID: sourceParentID})
	return nil
}

func (f *fakeFileOps) RenameFile(_ context.Context, _ int64, fileID, newName, parentID string) error {
	if err := f.renameErrs[fileID]; err != nil {
		return err
	}
	f.renames = append(f.renames, fakeRename{fileID: fileID, newName: newName, parent: parentID})
	return nil
}

func (f *fakeFileOps) DeleteFiles(_ context.Context, _ int64, fileIDs []string, parentID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletes = append(f.deletes, fakeDelete{fileIDs: append([]string(nil), fileIDs...), parent: parentID})
	return nil
}

func quarkAccount() *domain.Account {
	return &domain.Account{ID: 7, Name: "夸克", DriverType: "quark", IsActive: true}
}

func TestIngestMovesAndRenames(t *testing.T) {
	files := newFakeFileOps()
	configs := &fakeConfigStore{}
	svc := New(Options{
		Files:    files,
		Accounts: &fakeAccountReader{account: quarkAccount()},
		Configs:  configs,
	})

	result, err := svc.Ingest(context.Background(), IngestInput{
		AccountID:      7,
		SourceParentID: "inbox",
		FileIDs:        []string{"file-1"},
		Files: []IngestFile{
			{FileID: "file-1", OriginalName: "第一集.mkv", NewName: "剧名-S01E01.mkv"},
		},
		ParentID:   "base",
		TargetPath: []string{"剧名", "Season 01"},
		BasePath:   "/电视剧",
		BaseDirs:   []string{"电视剧"},
		Title:      "剧名",
		Season:     1,
		IsSeries:   true,
		ProgressID: "test-progress",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.Moved != 1 || result.Renamed != 1 || result.RenameSkipped != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(files.moves) != 1 {
		t.Fatalf("expected 1 move, got %d", len(files.moves))
	}
	move := files.moves[0]
	if move.sourceParentID != "inbox" || move.targetParentID != "folder-Season 01" {
		t.Fatalf("unexpected move: %+v", move)
	}
	if len(move.fileIDs) != 1 || move.fileIDs[0] != "file-1" {
		t.Fatalf("unexpected move ids: %+v", move.fileIDs)
	}
	if len(files.renames) != 1 {
		t.Fatalf("expected 1 rename, got %d", len(files.renames))
	}
	rename := files.renames[0]
	if rename.fileID != "file-1" || rename.newName != "剧名-S01E01.mkv" || rename.parent != "folder-Season 01" {
		t.Fatalf("unexpected rename: %+v", rename)
	}

	raw := configs.values[presetKey(7, 0)]
	if raw == "" {
		t.Fatal("expected preset to be saved")
	}
	var preset Preset
	if err := json.Unmarshal([]byte(raw), &preset); err != nil {
		t.Fatalf("decode preset: %v", err)
	}
	if preset.Title != "剧名" || preset.ParentID != "base" || !preset.IsSeries || preset.Season != 1 {
		t.Fatalf("unexpected preset: %+v", preset)
	}
	if progress, ok := svc.TransferProgress("test-progress"); !ok || progress.Phase != PhaseDone {
		t.Fatalf("unexpected progress: %+v, ok=%v", progress, ok)
	}
}

func TestIngestRejectsNonQuarkAccount(t *testing.T) {
	account := quarkAccount()
	account.DriverType = "guangya"
	svc := New(Options{
		Files:    newFakeFileOps(),
		Accounts: &fakeAccountReader{account: account},
		Configs:  &fakeConfigStore{},
	})
	_, err := svc.Ingest(context.Background(), IngestInput{
		AccountID:      7,
		SourceParentID: "inbox",
		FileIDs:        []string{"file-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "只支持夸克") {
		t.Fatalf("expected quark-only error, got %v", err)
	}
}

func TestIngestRejectsSameFolder(t *testing.T) {
	svc := New(Options{
		Files:    newFakeFileOps(),
		Accounts: &fakeAccountReader{account: quarkAccount()},
		Configs:  &fakeConfigStore{},
	})
	_, err := svc.Ingest(context.Background(), IngestInput{
		AccountID:      7,
		SourceParentID: "inbox",
		FileIDs:        []string{"file-1"},
		Files:          []IngestFile{{FileID: "file-1", OriginalName: "a.mkv"}},
		ParentID:       "inbox",
	})
	if err == nil || !strings.Contains(err.Error(), "相同") {
		t.Fatalf("expected same-folder error, got %v", err)
	}
}

func TestIngestContinuesWhenRenameFails(t *testing.T) {
	files := newFakeFileOps()
	files.renameErrs["file-1"] = errors.New("name conflict")
	svc := New(Options{
		Files:    files,
		Accounts: &fakeAccountReader{account: quarkAccount()},
		Configs:  &fakeConfigStore{},
	})
	result, err := svc.Ingest(context.Background(), IngestInput{
		AccountID:      7,
		SourceParentID: "inbox",
		FileIDs:        []string{"file-1", "file-2"},
		Files: []IngestFile{
			{FileID: "file-1", OriginalName: "a.mkv", NewName: "b.mkv"},
			{FileID: "file-2", OriginalName: "c.mkv", NewName: "d.mkv"},
		},
		ParentID:   "base",
		TargetPath: []string{"target"},
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.Renamed != 1 || result.RenameSkipped != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestNormalizeIngestFilesUsesFileIDsOrder(t *testing.T) {
	files := normalizeIngestFiles([]IngestFile{
		{FileID: "a", OriginalName: "a.mkv", NewName: "x.mkv"},
		{FileID: "b", OriginalName: "b.mkv"},
		{FileID: "a", OriginalName: "a.mkv", NewName: "y.mkv"},
	}, []string{"b", "a", "b"})
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].FileID != "b" || files[0].NewName != "b.mkv" {
		t.Fatalf("unexpected first file: %+v", files[0])
	}
	if files[1].FileID != "a" || files[1].NewName != "y.mkv" {
		t.Fatalf("unexpected second file: %+v", files[1])
	}
}

func TestBuildRenamePlanSkipsUnchangedAndDirs(t *testing.T) {
	plan := buildRenamePlan([]IngestFile{
		{FileID: "1", OriginalName: "a.mkv", NewName: "a.mkv"},
		{FileID: "2", OriginalName: "b", NewName: "b2", IsDir: true},
		{FileID: "3", OriginalName: "c.mkv", NewName: "d.mkv"},
		{FileID: "3", OriginalName: "c.mkv", NewName: "e.mkv"},
	})
	if len(plan) != 1 {
		t.Fatalf("expected 1 rename pair, got %d", len(plan))
	}
	if plan[0].FileID != "3" || plan[0].NewName != "d.mkv" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPresetRoundTrip(t *testing.T) {
	configs := &fakeConfigStore{}
	svc := New(Options{Configs: configs})
	want := Preset{
		AccountID:     9,
		ParentID:      "parent",
		BasePath:      "/电视剧",
		BaseDirs:      []string{"电视剧"},
		Title:         "剧名",
		Season:        2,
		IsSeries:      true,
		AutoStrm:      true,
		AutoScrape:    true,
		ScrapeTaskID:  3,
		RefreshEmby:   true,
		EmbyConfigID:  "emby",
		EmbyMode:      "library",
		EmbyLibraryID: "lib",
		RenameReplace: RenameReplace{
			Enabled: true,
			Rules:   []RenameRule{{Find: `\s+`, ReplaceWith: " "}},
		},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	configs.values = map[string]string{presetKey(9, 5): string(data)}

	got, err := svc.LoadPreset(context.Background(), 9, 5)
	if err != nil {
		t.Fatalf("LoadPreset() error = %v", err)
	}
	if got == nil {
		t.Fatal("expected preset")
	}
	if got.Title != want.Title || got.Season != want.Season || got.EmbyMode != want.EmbyMode {
		t.Fatalf("unexpected preset: %+v", got)
	}
	if got.RenameReplace.Find != `\s+` || !got.RenameReplace.Enabled {
		t.Fatalf("unexpected rename replace: %+v", got.RenameReplace)
	}
}

// 预设按收藏隔离：新收藏读不到别人的预设，因此页面完全使用默认值。
func TestPresetIsolatedPerFavorite(t *testing.T) {
	configs := &fakeConfigStore{}
	svc := New(Options{Configs: configs})
	raw, err := json.Marshal(Preset{AccountID: 9, ParentID: "parent", Title: "旧剧", Season: 3})
	if err != nil {
		t.Fatal(err)
	}
	configs.values = map[string]string{presetKey(9, 5): string(raw)}

	if got, err := svc.LoadPreset(context.Background(), 9, 5); err != nil || got == nil {
		t.Fatalf("同一收藏应读到预设: got=%+v err=%v", got, err)
	}
	got, err := svc.LoadPreset(context.Background(), 9, 6)
	if err != nil {
		t.Fatalf("LoadPreset() error = %v", err)
	}
	if got != nil {
		t.Fatalf("新收藏不应继承其它收藏的预设: %+v", got)
	}
}

func TestIngestBaseDirsFallsBackToBasePath(t *testing.T) {
	got := ingestBaseDirs(IngestInput{BasePath: "/电视剧/国产"})
	want := []string{"电视剧", "国产"}
	if len(got) != len(want) {
		t.Fatalf("unexpected dirs: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected dirs: %+v", got)
		}
	}
	if dirs := ingestBaseDirs(IngestInput{BaseDirs: []string{"指定"}, BasePath: "/忽略"}); len(dirs) != 1 || dirs[0] != "指定" {
		t.Fatalf("explicit dirs should win: %+v", dirs)
	}
}

func TestNormalizeTargetPath(t *testing.T) {
	got, err := normalizeTargetPath([]string{"剧名", " Season 01 "})
	if err != nil {
		t.Fatalf("normalizeTargetPath() error = %v", err)
	}
	if len(got) != 2 || got[0] != "剧名" || got[1] != "Season 01" {
		t.Fatalf("unexpected segments: %+v", got)
	}
	if _, err := normalizeTargetPath([]string{""}); err == nil {
		t.Fatal("expected validation error for empty segment")
	}
}

func TestIngestOnlyMovesFiles(t *testing.T) {
	files := newFakeFileOps()
	svc := New(Options{
		Files:    files,
		Accounts: &fakeAccountReader{account: quarkAccount()},
		Configs:  &fakeConfigStore{},
	})
	_, err := svc.Ingest(context.Background(), IngestInput{
		AccountID:      7,
		SourceParentID: "favorite-folder",
		FileIDs:        []string{"dir-1"},
		Files:          []IngestFile{{FileID: "dir-1", OriginalName: "合集", IsDir: true}},
		ParentID:       "base",
		TargetPath:     []string{"合集"},
	})
	if err == nil || !strings.Contains(err.Error(), "不支持移动文件夹") {
		t.Fatalf("expected folder-only error, got %v", err)
	}
	if len(files.moves) != 0 {
		t.Fatalf("folder should not be moved: %+v", files.moves)
	}
}

func TestIngestRenamesFavoriteFolderWhenTitleChanges(t *testing.T) {
	files := newFakeFileOps()
	favorites := &fakeFavoriteRepo{}
	svc := New(Options{
		Files:     files,
		Accounts:  &fakeAccountReader{account: quarkAccount()},
		Configs:   &fakeConfigStore{},
		Favorites: favorites,
	})
	result, err := svc.Ingest(context.Background(), IngestInput{
		AccountID:            7,
		FavoriteID:           3,
		SourceParentID:       "favorite-folder",
		SourceFolderParentID: "inbox",
		SourceFolderName:     "旧名字",
		FileIDs:              []string{"file-1"},
		Files:                []IngestFile{{FileID: "file-1", OriginalName: "01.mkv"}},
		ParentID:             "base",
		TargetPath:           []string{"新名字"},
		Title:                "新名字",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.Moved != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(files.renames) != 1 {
		t.Fatalf("expected folder rename, got %+v", files.renames)
	}
	rename := files.renames[0]
	if rename.fileID != "favorite-folder" || rename.newName != "新名字" || rename.parent != "inbox" {
		t.Fatalf("unexpected folder rename: %+v", rename)
	}
	if favorites.renamed[3] != "新名字" {
		t.Fatalf("expected favorite folder name update, got %+v", favorites.renamed)
	}
	if len(files.moves) != 1 || files.moves[0].sourceParentID != "favorite-folder" {
		t.Fatalf("unexpected moves: %+v", files.moves)
	}
}

func TestFavoriteCRUDAndValidation(t *testing.T) {
	favorites := &fakeFavoriteRepo{}
	files := newFakeFileOps()
	svc := New(Options{
		Files:     files,
		Accounts:  &fakeAccountReader{account: quarkAccount()},
		Favorites: favorites,
	})
	created, err := svc.CreateFavorite(context.Background(), FavoriteInput{
		AccountID:      7,
		Link:           "https://example.com/s/abc",
		FolderID:       "folder-1",
		ParentFolderID: "inbox",
		FolderName:     "作品",
	})
	if err != nil {
		t.Fatalf("CreateFavorite() error = %v", err)
	}
	if created.ID == 0 || created.Link == "" {
		t.Fatalf("unexpected favorite: %+v", created)
	}
	updated, err := svc.UpdateFavorite(context.Background(), created.ID, FavoriteInput{
		AccountID:      7,
		Link:           "https://example.com/s/def",
		FolderID:       "folder-1",
		ParentFolderID: "inbox",
		FolderName:     "作品",
	})
	if err != nil || updated.Link != "https://example.com/s/def" {
		t.Fatalf("UpdateFavorite() = %+v, %v", updated, err)
	}
	if _, err := svc.CreateFavorite(context.Background(), FavoriteInput{AccountID: 7, FolderID: "x"}); err == nil {
		t.Fatal("expected link validation error")
	}
	if err := svc.DeleteFavorite(context.Background(), created.ID, 7); err != nil {
		t.Fatalf("DeleteFavorite() error = %v", err)
	}
	if len(files.deletes) != 1 || len(files.deletes[0].fileIDs) != 1 || files.deletes[0].fileIDs[0] != "folder-1" {
		t.Fatalf("expected favorite folder deletion, got %+v", files.deletes)
	}
	if files.deletes[0].parent != "inbox" {
		t.Fatalf("unexpected delete parent: %+v", files.deletes[0])
	}
	if len(favorites.favorites) != 0 {
		t.Fatalf("expected favorite record deleted, got %+v", favorites.favorites)
	}
}

func TestDeleteFavoriteKeepsRecordWhenFolderDeleteFails(t *testing.T) {
	favorites := &fakeFavoriteRepo{}
	files := newFakeFileOps()
	// 文件夹仍在网盘上，此时删除失败必须保留收藏记录。
	files.items["inbox"] = []domain.FileItem{{ID: "folder-1", Name: "作品", IsDir: true}}
	files.deleteErr = errors.New("network down")
	svc := New(Options{
		Files:     files,
		Accounts:  &fakeAccountReader{account: quarkAccount()},
		Favorites: favorites,
	})
	created, err := svc.CreateFavorite(context.Background(), FavoriteInput{
		AccountID:      7,
		Link:           "https://example.com/s/abc",
		FolderID:       "folder-1",
		ParentFolderID: "inbox",
		FolderName:     "作品",
	})
	if err != nil {
		t.Fatalf("CreateFavorite() error = %v", err)
	}
	if err := svc.DeleteFavorite(context.Background(), created.ID, 7); err == nil {
		t.Fatal("expected folder delete error")
	}
	if len(favorites.favorites) != 1 {
		t.Fatalf("favorite should remain after folder delete failure, got %+v", favorites.favorites)
	}
}

// 收藏文件夹已被手动删除时，网盘删除会报错；此时应忽略报错并删除收藏记录。
func TestDeleteFavoriteIgnoresMissingFolderError(t *testing.T) {
	favorites := &fakeFavoriteRepo{}
	files := newFakeFileOps()
	files.deleteErr = domain.Errorf(domain.CodeDriverError, "夸克接口错误(41004)：文件不存在")
	svc := New(Options{
		Files:     files,
		Accounts:  &fakeAccountReader{account: quarkAccount()},
		Favorites: favorites,
	})
	created, err := svc.CreateFavorite(context.Background(), FavoriteInput{
		AccountID:      7,
		Link:           "https://example.com/s/abc",
		FolderID:       "folder-1",
		ParentFolderID: "inbox",
		FolderName:     "作品",
	})
	if err != nil {
		t.Fatalf("CreateFavorite() error = %v", err)
	}
	if err := svc.DeleteFavorite(context.Background(), created.ID, 7); err != nil {
		t.Fatalf("文件夹已不存在时不应报错: %v", err)
	}
	if len(favorites.favorites) != 0 {
		t.Fatalf("收藏记录应被删除, got %+v", favorites.favorites)
	}
}

func TestIngestIgnoresFolderEntriesWhenFilesPresent(t *testing.T) {
	files := newFakeFileOps()
	svc := New(Options{
		Files:    files,
		Accounts: &fakeAccountReader{account: quarkAccount()},
		Configs:  &fakeConfigStore{},
	})
	result, err := svc.Ingest(context.Background(), IngestInput{
		AccountID:      7,
		SourceParentID: "favorite-folder",
		FileIDs:        []string{"file-1", "dir-1"},
		Files: []IngestFile{
			{FileID: "file-1", OriginalName: "01.mkv"},
			{FileID: "dir-1", OriginalName: "花絮", IsDir: true},
		},
		ParentID:   "base",
		TargetPath: []string{"作品"},
		Title:      "作品",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.Moved != 1 {
		t.Fatalf("expected only one file moved, got %+v", result)
	}
	if len(files.moves) != 1 || len(files.moves[0].fileIDs) != 1 || files.moves[0].fileIDs[0] != "file-1" {
		t.Fatalf("folder should not be moved: %+v", files.moves)
	}
}

func TestFlattenFavoriteMovesRecursivelyAndDeletesFolders(t *testing.T) {
	files := newFakeFileOps()
	files.items["root"] = []domain.FileItem{
		{ID: "root-file", Name: "root.mkv"},
		{ID: "folder-a", Name: "A", IsDir: true},
		{ID: "folder-b", Name: "B", IsDir: true},
	}
	files.items["folder-a"] = []domain.FileItem{
		{ID: "a-ep1", Name: "ep1.mkv"},
		{ID: "folder-c", Name: "C", IsDir: true},
	}
	files.items["folder-c"] = []domain.FileItem{
		{ID: "c-root", Name: "root.mkv"},
		{ID: "c-ep1", Name: "ep1.mkv"},
	}
	files.items["folder-b"] = []domain.FileItem{
		{ID: "b-file", Name: "B.mkv"},
	}
	favorites := &fakeFavoriteRepo{favorites: map[int64]*domain.QuarkIngestFavorite{
		3: {ID: 3, AccountID: 7, FolderID: "root", ParentFolderID: "inbox", FolderName: "作品"},
	}}
	svc := New(Options{
		Files:     files,
		Accounts:  &fakeAccountReader{account: quarkAccount()},
		Favorites: favorites,
	})

	result, err := svc.FlattenFavorite(context.Background(), 7, 3)
	if err != nil {
		t.Fatalf("FlattenFavorite() error = %v", err)
	}
	if result.Moved != 4 || result.Renamed != 2 || result.DeletedFolders != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(files.renames) != 2 {
		t.Fatalf("expected 2 renames, got %+v", files.renames)
	}
	if files.renames[0].fileID != "c-root" || files.renames[0].newName != "root (1).mkv" {
		t.Fatalf("unexpected first rename: %+v", files.renames[0])
	}
	if files.renames[1].fileID != "c-ep1" || files.renames[1].newName != "ep1 (1).mkv" {
		t.Fatalf("unexpected second rename: %+v", files.renames[1])
	}
	if len(files.moves) != 3 {
		t.Fatalf("expected 3 move groups, got %+v", files.moves)
	}
	for _, move := range files.moves {
		if move.targetParentID != "root" {
			t.Fatalf("unexpected move target: %+v", move)
		}
	}
	if len(files.deletes) != 2 {
		t.Fatalf("expected 2 delete groups, got %+v", files.deletes)
	}
	if len(files.deletes[0].fileIDs) != 1 || files.deletes[0].fileIDs[0] != "folder-c" || files.deletes[0].parent != "folder-a" {
		t.Fatalf("expected deepest folder first, got %+v", files.deletes)
	}
	if len(files.deletes[1].fileIDs) != 2 {
		t.Fatalf("expected root folders grouped together, got %+v", files.deletes[1])
	}
}

func TestUniqueFlattenName(t *testing.T) {
	used := map[string]struct{}{"movie.mkv": {}}
	if got := uniqueFlattenName("Movie.MKV", used, nil); got != "Movie (1).MKV" {
		t.Fatalf("unexpected duplicate name: %q", got)
	}
	if got := uniqueFlattenName("movie (1).mkv", used, nil); got != "movie (1) (1).mkv" {
		t.Fatalf("unexpected second duplicate name: %q", got)
	}
	if got := uniqueFlattenName("README", used, nil); got != "README" {
		t.Fatalf("unexpected extensionless name: %q", got)
	}

	used["show.mkv"] = struct{}{}
	sourceReserved := map[string]struct{}{"show (1).mkv": {}, "show (2).mkv": {}}
	if got := uniqueFlattenName("show.mkv", used, sourceReserved); got != "show (3).mkv" {
		t.Fatalf("expected source sibling names to be skipped, got %q", got)
	}
}
