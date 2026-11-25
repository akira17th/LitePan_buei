package strmscrape

import (
	"encoding/json"
	"testing"
)

type fakeImageSizer struct{}

func (fakeImageSizer) ImageURL(path, size string) string {
	return size + path
}

func TestParseTMDBResultsNormalizesMovieAndTV(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"id": 1, "media_type": "movie", "title": "电影甲", "original_title": "Movie A", "release_date": "2019-05-01", "overview": "简介", "poster_path": "/a.jpg", "vote_average": 7.5}`),
		json.RawMessage(`{"id": 2, "media_type": "tv", "name": "剧集乙", "original_name": "Show B", "first_air_date": "2023-09-12"}`),
		json.RawMessage(`{"id": 0}`),
	}
	results := parseTMDBResults(fakeImageSizer{}, raw)
	if len(results) != 2 {
		t.Fatalf("应跳过无效结果：%#v", results)
	}
	if results[0].Title != "电影甲" || results[0].Year != 2019 || results[0].MediaType != "movie" || results[0].PosterURL != "w342/a.jpg" {
		t.Fatalf("电影结果解析异常：%#v", results[0])
	}
	if results[1].Title != "剧集乙" || results[1].Year != 2023 || results[1].MediaType != "tv" || results[1].OriginalTitle != "Show B" {
		t.Fatalf("剧集结果解析异常：%#v", results[1])
	}
}

func TestYearFromDate(t *testing.T) {
	cases := map[string]int{"2024-01-02": 2024, "1999": 1999, "": 0, "abcd": 0}
	for input, want := range cases {
		if got := yearFromDate(input); got != want {
			t.Fatalf("yearFromDate(%q) = %d，期望 %d", input, got, want)
		}
	}
}

func TestSortTMDBResultsByYearDesc(t *testing.T) {
	results := []TMDBSearchResult{
		{ID: 1, Title: "旧片", Year: 2005, Rating: 9},
		{ID: 2, Title: "新剧", Year: 2024, Rating: 6},
		{ID: 3, Title: "中间", Year: 2018, Rating: 8},
	}
	sortTMDBResults(results)
	want := []int{2024, 2018, 2005}
	for i, year := range want {
		if results[i].Year != year {
			t.Fatalf("结果未按年份倒序：%#v", results)
		}
	}
}
