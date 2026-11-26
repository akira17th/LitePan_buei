package strmscrape

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeDateValue(t *testing.T) {
	cases := map[string]string{
		"2023-08-30":           "2023-08-30",
		" 2023-08-30 ":         "2023-08-30",
		"2023-08-30(中国大陆)":     "2023-08-30",
		"2023-08-30T00:00:00Z": "2023-08-30",
		"":                     "",
		"2023/08/30":           "",
		"20230830":             "",
		"2023-13-45":           "",
		"上映日期未知":               "",
	}
	for in, want := range cases {
		if got := normalizeDateValue(in); got != want {
			t.Fatalf("normalizeDateValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteWorkNFOWithDates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvshow.nfo")
	year := 2023
	if err := writeWorkNFOWithDates("tvshow", path, "漫长的季节", "35588177", "剧情", &year, MediaTypeTV, "2023-04-22", "2023-05-01", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<tvshow>", "<premiered>2023-04-22</premiered>", "<enddate>2023-05-01</enddate>", "<year>2023</year>"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("NFO 缺少 %s：%s", want, data)
		}
	}

	// 电影没有完结日期。
	moviePath := filepath.Join(dir, "movie.nfo")
	if err := writeWorkNFOWithDates("movie", moviePath, "奥本海默", "1", "", nil, MediaTypeMovie, "2023-08-30", "2023-09-30", nil); err != nil {
		t.Fatal(err)
	}
	movieData, err := os.ReadFile(moviePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(movieData), "<enddate>") {
		t.Fatalf("电影 NFO 不应写完结日期：%s", movieData)
	}
	if !strings.Contains(string(movieData), "<premiered>2023-08-30</premiered>") {
		t.Fatalf("电影 NFO 缺少发行日期：%s", movieData)
	}
}

func TestPatchNFODatesFillsMissingOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvshow.nfo")
	// 已有 <premiered> 空节点 + 用户手写的 enddate，只补空的那个。
	writeTestFile(t, path, `<?xml version="1.0" encoding="UTF-8"?>
<tvshow>
  <title>示例剧</title>
  <premiered></premiered>
  <enddate>2020-01-01</enddate>
</tvshow>
`)
	changed, err := patchNFODates(path, MediaTypeTV, "2023-04-22", "2023-05-01")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("缺 premiered 应补写")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "<premiered>2023-04-22</premiered>") {
		t.Fatalf("premiered 未补写：%s", data)
	}
	if !strings.Contains(string(data), "<enddate>2020-01-01</enddate>") {
		t.Fatalf("已有 enddate 不应被覆盖：%s", data)
	}

	// 再补一次：无变化。
	changed, err = patchNFODates(path, MediaTypeTV, "2023-04-22", "2023-05-01")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("已补齐后不应再改文件")
	}
}

func TestPatchNFODatesInsertsNodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.nfo")
	writeTestFile(t, path, "<?xml version=\"1.0\"?>\n<movie>\n  <title>示例电影</title>\n</movie>\n")
	if _, err := patchNFODates(path, MediaTypeMovie, "2023-08-30", "2023-09-30"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "<premiered>2023-08-30</premiered>") {
		t.Fatalf("premiered 未插入：%s", data)
	}
	if strings.Contains(string(data), "<enddate>") {
		t.Fatalf("电影不应插入 enddate：%s", data)
	}
	// 节点必须留在根节点内部。
	if strings.Index(string(data), "</movie>") < strings.Index(string(data), "<premiered>") {
		t.Fatalf("premiered 应位于 </movie> 之前：%s", data)
	}
}

func TestPatchNFODatesSkipsNonStandardNFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.nfo")
	writeTestFile(t, path, "压制组信息文本，不是标准 NFO")
	changed, err := patchNFODates(path, MediaTypeMovie, "2023-08-30", "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("非标准 NFO 不应修改")
	}
}

func TestIndexStoresDates(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{dataDir: dir}
	db, err := openTaskIndexDB(svc.indexPath(11))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	it := Item{
		ID: "tv", Title: "示例剧", MediaType: MediaTypeTV, Status: ItemStatusOK,
		ReleaseDate: "2023-04-22", EndDate: "2023-05-01", TVState: TVStateEnded,
	}
	if err := upsertItemTx(tx, it, ""); err != nil {
		t.Fatal(err)
	}
	if err := writeIndexMeta(tx, "schema", indexSchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := writeIndexMeta(tx, "root", dir); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	res, err := svc.listIndexItems(11, ItemListQuery{Limit: defaultItemListLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %+v", res.Items)
	}
	got := res.Items[0]
	if got.ReleaseDate != "2023-04-22" || got.EndDate != "2023-05-01" {
		t.Fatalf("日期未落库：release=%q end=%q", got.ReleaseDate, got.EndDate)
	}
}

func TestDecodeTMDBInfoDates(t *testing.T) {
	movie := json.RawMessage(`{"id": 1, "title": "奥本海默", "release_date": "2023-08-30"}`)
	info, err := decodeTMDBInfo(movie, MediaTypeMovie)
	if err != nil {
		t.Fatal(err)
	}
	if info.ReleaseDate != "2023-08-30" {
		t.Fatalf("电影发行日期 = %q", info.ReleaseDate)
	}
	if info.EndDate != "" {
		t.Fatalf("电影不应有完结日期：%q", info.EndDate)
	}

	ended := json.RawMessage(`{"id": 2, "name": "漫长的季节", "first_air_date": "2023-04-22", "last_air_date": "2023-05-01", "status": "Ended"}`)
	info, err = decodeTMDBInfo(ended, MediaTypeTV)
	if err != nil {
		t.Fatal(err)
	}
	if info.ReleaseDate != "2023-04-22" || info.EndDate != "2023-05-01" {
		t.Fatalf("已完结剧集日期 = %q / %q", info.ReleaseDate, info.EndDate)
	}

	// 连载中的剧集 last_air_date 是最新已播集，不能当完结日期。
	airing := json.RawMessage(`{"id": 3, "name": "在播剧", "first_air_date": "2026-09-01", "last_air_date": "2026-11-20", "status": "Returning Series"}`)
	info, err = decodeTMDBInfo(airing, MediaTypeTV)
	if err != nil {
		t.Fatal(err)
	}
	if info.ReleaseDate != "2026-09-01" {
		t.Fatalf("在播剧首播日期 = %q", info.ReleaseDate)
	}
	if info.EndDate != "" {
		t.Fatalf("在播剧不应有完结日期：%q", info.EndDate)
	}

	canceled := json.RawMessage(`{"id": 4, "name": "被砍剧", "first_air_date": "2024-01-01", "last_air_date": "2024-02-01", "status": "Canceled"}`)
	info, err = decodeTMDBInfo(canceled, MediaTypeTV)
	if err != nil {
		t.Fatal(err)
	}
	if info.EndDate != "2024-02-01" {
		t.Fatalf("被砍剧完结日期 = %q", info.EndDate)
	}
}
