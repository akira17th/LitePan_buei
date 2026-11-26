package strmscrape

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// buei:rating —— 把豆瓣评分写进作品 NFO 的「社区评分」字段。
//
// Emby / Jellyfin 读取 NFO 根级 <rating>（0-10）作为 CommunityRating、<votes> 作为评分人数；
// 已有 <ratings> 集合（Kodi 新格式）时不动它，只同步根级节点，
// 避免把 <ratings> 里的 <rating name="..."><value>…</value></rating> 结构改坏。

// ItemRating 是某个作品要写入 NFO 的社区评分与豆瓣条目 ID。
type ItemRating struct {
	ItemID   string
	Rating   float64
	Votes    int
	DoubanID string
}

// nfoCommunity 是 NFO 里的社区评分与豆瓣条目 ID。
type nfoCommunity struct {
	Rating   float64
	Votes    int
	DoubanID string
}

var (
	nfoRatingsBlockRe = regexp.MustCompile(`(?is)<ratings\b[^>]*>.*?</ratings\s*>`)
	nfoDoubanUIDRe    = regexp.MustCompile(`(?is)<uniqueid\b[^>]*type\s*=\s*"douban"[^>]*>(.*?)</uniqueid\s*>`)
)

// UpdateItemRatings 把评分批量写进任务内对应作品的 NFO。
// 只改 NFO，不动索引与网盘；找不到 NFO 或 NFO 不是标准 movie/tvshow 的作品跳过。
func (s *Service) UpdateItemRatings(ctx context.Context, strmTaskID int64, ratings []ItemRating) error {
	if strmTaskID <= 0 || len(ratings) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, root, err := s.resolveTask(ctx, strmTaskID)
	if err != nil {
		return err
	}
	works, err := scanWorks(root)
	if err != nil {
		return err
	}
	works = filterWorksByScope(works, s.GetScope(strmTaskID).ExcludedDirs)
	byID := make(map[string]workGroup, len(works))
	for _, g := range works {
		byID[pathToItemID(g.relKey)] = g
	}
	written := 0
	for _, r := range ratings {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.TrimSpace(r.ItemID) == "" {
			continue
		}
		if r.Rating <= 0 && strings.TrimSpace(r.DoubanID) == "" {
			continue
		}
		g, ok := byID[r.ItemID]
		if !ok {
			continue
		}
		mediaType := resolveWorkMediaType(g)
		nfo, _ := workMetaPaths(g, mediaType)
		if _, err := patchNFOCommunity(nfo, nfoCommunity{Rating: r.Rating, Votes: r.Votes, DoubanID: r.DoubanID}); err != nil {
			if s.log != nil {
				s.log.Warn("写入 NFO 社区评分失败，已跳过", "nfo", nfo, "err", err)
			}
			continue
		}
		written++
	}
	if written > 0 && s.log != nil {
		s.log.Debug("已写入 NFO 社区评分", "task_id", strmTaskID, "count", written)
	}
	return nil
}

// readNFOCommunity 读取根级 <rating> / <votes> 与豆瓣 ID，用于重写 NFO 前保留。
func readNFOCommunity(path string) (nfoCommunity, bool) {
	var out nfoCommunity
	data, err := os.ReadFile(path)
	if err != nil {
		return out, false
	}
	text := string(data)
	protected := nfoRatingsBlockRe.FindAllStringIndex(text, -1)
	inProtected := func(pos int) bool {
		for _, span := range protected {
			if pos >= span[0] && pos < span[1] {
				return true
			}
		}
		return false
	}
	for _, loc := range nfoTagRe("rating").FindAllStringSubmatchIndex(text, -1) {
		if inProtected(loc[0]) {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(text[loc[2]:loc[3]]), 64); err == nil && v > 0 {
			out.Rating = v
		}
		break
	}
	for _, loc := range nfoTagRe("votes").FindAllStringSubmatchIndex(text, -1) {
		if inProtected(loc[0]) {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSpace(text[loc[2]:loc[3]])); err == nil && v > 0 {
			out.Votes = v
		}
		break
	}
	for _, loc := range nfoTagRe("doubanid").FindAllStringSubmatchIndex(text, -1) {
		if inProtected(loc[0]) {
			continue
		}
		out.DoubanID = strings.TrimSpace(text[loc[2]:loc[3]])
		break
	}
	if out.DoubanID == "" {
		if loc := nfoDoubanUIDRe.FindStringSubmatchIndex(text); loc != nil {
			out.DoubanID = strings.TrimSpace(text[loc[2]:loc[3]])
		}
	}
	return out, out.Rating > 0 || out.DoubanID != ""
}

// patchNFOCommunity 写入/更新根级 <rating> / <votes> / <doubanid> / <uniqueid type="douban">，
// 返回是否改动文件。
func patchNFOCommunity(path string, community nfoCommunity) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	lower := strings.ToLower(string(data))
	if !strings.Contains(lower, "<movie") && !strings.Contains(lower, "<tvshow") {
		return false, nil
	}
	value := ""
	if community.Rating > 0 {
		value = strconv.FormatFloat(community.Rating, 'f', 1, 64)
	}
	votesValue := ""
	if community.Votes > 0 {
		votesValue = strconv.Itoa(community.Votes)
	}
	doubanID := strings.TrimSpace(community.DoubanID)

	text := string(data)
	protected := nfoRatingsBlockRe.FindAllStringIndex(text, -1)
	inProtected := func(pos int) bool {
		for _, span := range protected {
			if pos >= span[0] && pos < span[1] {
				return true
			}
		}
		return false
	}

	changed := false
	// apply 覆盖所有根级同名节点，返回是否存在（<ratings> 集合内的同名节点不动）。
	apply := func(tag, next string) bool {
		if next == "" {
			return false
		}
		matches := nfoTagRe(tag).FindAllStringSubmatchIndex(text, -1)
		found := false
		// 从后往前替换，避免前面的改动影响后面的下标。
		for i := len(matches) - 1; i >= 0; i-- {
			loc := matches[i]
			if inProtected(loc[0]) {
				continue
			}
			found = true
			if strings.TrimSpace(text[loc[2]:loc[3]]) == next {
				continue
			}
			text = text[:loc[2]] + next + text[loc[3]:]
			changed = true
		}
		return found
	}
	insert := func(tag, next string) {
		if next == "" {
			return
		}
		updated, ok := insertNFONode([]byte(text), tag, next)
		if !ok {
			return
		}
		text = string(updated)
		changed = true
	}
	// applyUniqueID 只改 <uniqueid type="douban"> 的内部值。
	applyUniqueID := func(next string) bool {
		loc := nfoDoubanUIDRe.FindStringSubmatchIndex(text)
		if loc == nil {
			return false
		}
		if strings.TrimSpace(text[loc[2]:loc[3]]) == next {
			return true
		}
		text = text[:loc[2]] + next + text[loc[3]:]
		changed = true
		return true
	}
	if !apply("rating", value) {
		insert("rating", value)
	}
	if !apply("votes", votesValue) {
		insert("votes", votesValue)
	}
	if doubanID != "" {
		if !apply("doubanid", doubanID) {
			insert("doubanid", doubanID)
		}
		if !applyUniqueID(doubanID) {
			if updated, ok := insertNFORaw([]byte(text), `<uniqueid type="douban" default="false">`+doubanID+`</uniqueid>`); ok {
				text = string(updated)
				changed = true
			}
		}
	}
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(text), 0o644)
}
