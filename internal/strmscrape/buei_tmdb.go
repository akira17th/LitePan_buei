package strmscrape

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
)

// buei:emby-library —— 「Emby 媒体库 → 项目目录」用的 TMDB 搜索与手动匹配。

// TMDBSearchResult 是 TMDB 搜索结果（已整理成前端直接可用）。
type TMDBSearchResult struct {
	ID            int     `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title,omitempty"`
	Year          int     `json:"year,omitempty"`
	Overview      string  `json:"overview,omitempty"`
	PosterURL     string  `json:"poster_url,omitempty"`
	Rating        float64 `json:"rating,omitempty"`
}

// SearchTMDB 使用刮削配置里的 TMDB Key 搜索作品，结果按年份倒序。
func (s *Service) SearchTMDB(ctx context.Context, query string, year *int, mediaType string) ([]TMDBSearchResult, error) {
	client := s.newTMDBClient()
	if client == nil {
		return nil, domain.Errorf(domain.CodeValidation, "未配置 TMDB API Key")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, domain.Errorf(domain.CodeValidation, "请输入要搜索的名称")
	}
	raw, err := client.Search(ctx, query, year, mediaType)
	if err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "TMDB 搜索失败：%v", err)
	}
	out := parseTMDBResults(client, raw)
	sortTMDBResults(out)
	return out, nil
}

// sortTMDBResults 按年份倒序（同一年按评分倒序）排列搜索结果。
func sortTMDBResults(results []TMDBSearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Year != results[j].Year {
			return results[i].Year > results[j].Year
		}
		return results[i].Rating > results[j].Rating
	})
}

type tmdbImageSizer interface {
	ImageURL(imagePath, size string) string
}

// parseTMDBResults 把 TMDB 原始搜索结果整理成前端直接可用的结构（电影 / 剧集字段归一化）。
func parseTMDBResults(images tmdbImageSizer, raw []json.RawMessage) []TMDBSearchResult {
	out := make([]TMDBSearchResult, 0, len(raw))
	for _, item := range raw {
		var payload map[string]any
		if err := json.Unmarshal(item, &payload); err != nil {
			continue
		}
		id := int(rawFloat(payload["id"]))
		if id <= 0 {
			continue
		}
		kind := strings.TrimSpace(rawString(payload["media_type"]))
		title := strings.TrimSpace(rawString(payload["title"]))
		original := strings.TrimSpace(rawString(payload["original_title"]))
		date := rawString(payload["release_date"])
		if kind == "tv" || (title == "" && rawString(payload["name"]) != "") {
			kind = "tv"
			title = firstNonEmpty(title, rawString(payload["name"]))
			original = firstNonEmpty(original, rawString(payload["original_name"]))
			date = firstNonEmpty(date, rawString(payload["first_air_date"]))
		}
		if kind == "" {
			kind = "movie"
		}
		out = append(out, TMDBSearchResult{
			ID:            id,
			MediaType:     kind,
			Title:         title,
			OriginalTitle: original,
			Year:          yearFromDate(date),
			Overview:      strings.TrimSpace(rawString(payload["overview"])),
			PosterURL:     images.ImageURL(rawString(payload["poster_path"]), "w342"),
			Rating:        rawFloat(payload["vote_average"]),
		})
	}
	return out
}

// MatchTMDBItem 同步用指定 TMDB ID 重新刮削一个项目：
// 与 Rematch 逻辑一致，但等待刮削结束后才返回，便于调用方立刻刷新索引与媒体库。
func (s *Service) MatchTMDBItem(ctx context.Context, req RematchRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" || strings.TrimSpace(req.TMDBID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中，请稍后再试")
	}
	defer s.operationMu.Unlock()
	releaseFiles := func() {}
	if s.strm != nil {
		var ok bool
		releaseFiles, ok = s.strm.TryBeginTaskFileOperation(req.StrmTaskID)
		if !ok {
			return nil, domain.Errorf(domain.CodeValidation, "该 STRM 任务正在运行，请稍后再试")
		}
	}
	defer releaseFiles()

	root, g, err := s.prepareRematchTarget(ctx, req.StrmTaskID, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := strings.ToLower(strings.TrimSpace(req.MediaType))
	if mediaType == "" {
		mediaType = resolveWorkMediaType(g)
	}
	if s.newTMDBClient() == nil {
		return nil, domain.Errorf(domain.CodeValidation, "未配置 TMDB API Key")
	}
	display := strings.TrimSpace(req.Title)
	if display == "" {
		display = workDisplayName(g)
	}
	overwrite := s.overwriteForMatch(workTMDBIDMatches(g, mediaType, req.TMDBID))

	s.setProgress(func(p *Progress) {
		p.Running = true
		p.TaskID = req.StrmTaskID
		p.Total = 1
		p.Done = 0
		p.Failed = 0
		p.Error = ""
		p.CurrentItemID = req.ItemID
		p.StartedAt = time.Now().Format(time.RFC3339)
		p.Message = "正在刮削：" + display
	})
	item, applyErr := s.applyRematch(ctx, req, root, g, mediaType, overwrite)
	s.setProgress(func(p *Progress) {
		p.Running = false
		p.CurrentItemID = ""
		if applyErr != nil {
			p.Failed++
			p.Error = applyErr.Error()
			p.Message = "重新匹配失败：" + display
			return
		}
		p.Done++
		p.ItemRevision++
		p.UpdatedItem = item
		p.Message = "已重新匹配：" + display
	})
	if applyErr != nil {
		return nil, applyErr
	}
	return item, nil
}

func rawString(value any) string {
	text, _ := value.(string)
	return text
}

func rawFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func yearFromDate(date string) int {
	date = strings.TrimSpace(date)
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return year
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
