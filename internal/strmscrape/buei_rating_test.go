package strmscrape

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/strm"
)

func TestPatchNFOCommunityInsertsNodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvshow.nfo")
	writeTestFile(t, path, "<?xml version=\"1.0\"?>\n<tvshow>\n  <title>示例剧</title>\n</tvshow>\n")
	changed, err := patchNFOCommunity(path, nfoCommunity{Rating: 8.76, Votes: 12345, DoubanID: "1292052"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("应写入评分")
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "<rating>8.8</rating>") {
		t.Fatalf("rating 未写入或未保留一位小数：%s", text)
	}
	if !strings.Contains(text, "<votes>12345</votes>") {
		t.Fatalf("votes 未写入：%s", text)
	}
	if strings.Index(text, "</tvshow>") < strings.Index(text, "<rating>") {
		t.Fatalf("节点应位于根节点内部：%s", text)
	}
}

func TestPatchNFOCommunityOverwritesLegacyNodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.nfo")
	writeTestFile(t, path, `<?xml version="1.0"?>
<movie>
  <title>示例电影</title>
  <rating>6.5</rating>
  <votes>100</votes>
</movie>
`)
	changed, err := patchNFOCommunity(path, nfoCommunity{Rating: 9.0, Votes: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("应更新已有评分")
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "<rating>9.0</rating>") || strings.Contains(text, "<rating>6.5</rating>") {
		t.Fatalf("rating 未覆盖：%s", text)
	}
	if !strings.Contains(text, "<votes>200</votes>") || strings.Contains(text, "<votes>100</votes>") {
		t.Fatalf("votes 未覆盖：%s", text)
	}
	if strings.Count(text, "<rating>") != 1 {
		t.Fatalf("不应产生重复节点：%s", text)
	}
}

func TestPatchNFOCommunityKeepsRatingsBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.nfo")
	writeTestFile(t, path, `<?xml version="1.0"?>
<movie>
  <title>示例电影</title>
  <ratings>
    <rating name="themoviedb" max="10" default="true">
      <value>7.2</value>
      <votes>900</votes>
    </rating>
  </ratings>
</movie>
`)
	if _, err := patchNFOCommunity(path, nfoCommunity{Rating: 8.4, Votes: 321}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	// <ratings> 集合内部必须原样保留。
	if !strings.Contains(text, `<rating name="themoviedb" max="10" default="true">`) ||
		!strings.Contains(text, "<value>7.2</value>") {
		t.Fatalf("<ratings> 集合被改坏：%s", text)
	}
	// 根级节点单独写入。
	if !strings.Contains(text, "<rating>8.4</rating>") || !strings.Contains(text, "<votes>321</votes>") {
		t.Fatalf("根级评分未写入：%s", text)
	}
}

func TestPatchNFOCommunitySkipsMissingOrNonStandard(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "none.nfo")
	if changed, err := patchNFOCommunity(missing, nfoCommunity{Rating: 8, Votes: 1}); err != nil || changed {
		t.Fatalf("NFO 不存在时应跳过：changed=%v err=%v", changed, err)
	}
	plain := filepath.Join(dir, "plain.nfo")
	writeTestFile(t, plain, "压制组信息文本")
	if changed, err := patchNFOCommunity(plain, nfoCommunity{Rating: 8, Votes: 1}); err != nil || changed {
		t.Fatalf("非标准 NFO 应跳过：changed=%v err=%v", changed, err)
	}
}

func TestUpdateItemRatingsWritesWorkNFO(t *testing.T) {
	strmRoot := t.TempDir()
	task := &domain.StrmTask{ID: 12, OutputFolder: "示例输出"}
	root := strm.TaskOutputDir(strmRoot, task.OutputFolder)
	workDir := filepath.Join(root, "示例剧 (2023)")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workDir, "S01E01.strm"), "http://example.com/1.mkv")
	writeTestFile(t, filepath.Join(workDir, "tvshow.nfo"), "<?xml version=\"1.0\"?>\n<tvshow>\n  <title>示例剧</title>\n  <tmdbid>35588177</tmdbid>\n</tvshow>\n")

	strmSvc := strm.NewService(strm.ServiceOptions{
		Repo:    &rematchTaskRepo{task: task},
		StrmDir: strmRoot,
	})
	svc := New(Options{Strm: strmSvc, StrmDir: strmRoot, DataDir: t.TempDir()})
	err := svc.UpdateItemRatings(context.Background(), task.ID, []ItemRating{
		{ItemID: pathToItemID("示例剧 (2023)"), Rating: 9.4, Votes: 209936, DoubanID: "35588177"},
		{ItemID: "不存在", Rating: 8, Votes: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, "tvshow.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "<rating>9.4</rating>") || !strings.Contains(text, "<votes>209936</votes>") {
		t.Fatalf("NFO 未写入评分：%s", text)
	}
	if !strings.Contains(text, "<doubanid>35588177</doubanid>") {
		t.Fatalf("NFO 未写入豆瓣 ID：%s", text)
	}
	if !strings.Contains(text, `<uniqueid type="douban" default="false">35588177</uniqueid>`) {
		t.Fatalf("NFO 未写入豆瓣 uniqueid：%s", text)
	}
}

func TestReadNFOCommunityIgnoresRatingsBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.nfo")
	writeTestFile(t, path, `<?xml version="1.0"?>
<movie>
  <ratings>
    <rating name="themoviedb" max="10" default="true">
      <value>7.2</value>
      <votes>900</votes>
    </rating>
  </ratings>
</movie>
`)
	if _, ok := readNFOCommunity(path); ok {
		t.Fatal("只有 <ratings> 集合时不应读到根级社区评分")
	}
	if _, err := patchNFOCommunity(path, nfoCommunity{Rating: 8.4, Votes: 321}); err != nil {
		t.Fatal(err)
	}
	community, ok := readNFOCommunity(path)
	if !ok || community.Rating != 8.4 || community.Votes != 321 {
		t.Fatalf("回读 = %+v %v", community, ok)
	}
}
