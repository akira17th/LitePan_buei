package strmscrape

import (
	"context"
	"encoding/xml"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"litepan/internal/mediaorganize/tmdb"
)

// buei:dates —— 作品「发行日期」「完结日期」。
//
// 数据来源只有 TMDB（豆瓣没有剧集完结日期，电影上映日期也常是中国大陆重映）：
//   - 发行日期：电影取 release_date，剧集取 first_air_date（首播）；
//   - 完结日期：仅剧集，取 last_air_date，且要求 TMDB status 为 Ended / Canceled，
//     否则「最新已播集日期」会被误当成完结日期。
//
// 日期写进作品 NFO 的 <premiered> / <enddate>（Emby / Kodi 都识别），
// 这样 STRM 索引重建（索引只做缓存）时可以从磁盘恢复，无需重新请求 TMDB。

const dateOnlyLayout = "2006-01-02"

var datePrefixRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)

// normalizeDateValue 只保留 YYYY-MM-DD 部分，其它格式（含空值）一律返回空串。
func normalizeDateValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if m := datePrefixRe.FindStringSubmatch(raw); m != nil {
		if _, err := parseDateValue(m[1]); err == nil {
			return m[1]
		}
	}
	return ""
}

func parseDateValue(raw string) (time.Time, error) {
	return time.Parse(dateOnlyLayout, strings.TrimSpace(raw))
}

// workDates 读取作品 NFO 里已写入的发行日期 / 完结日期。
func workDates(g workGroup, mediaType string) (releaseDate, endDate string) {
	meta, ok := readWorkNFOMeta(g, mediaType)
	if !ok {
		return "", ""
	}
	return meta.Premiered, meta.EndDate
}

// workNeedsDateBackfill 判断已有 NFO 是否缺发行日期（缺则本次刮削顺带补一次）。
// 完结日期不做触发条件：连载中的剧集本来就没有完结日期，否则每轮都会重复请求 TMDB。
func workNeedsDateBackfill(g workGroup, mediaType string) bool {
	if !workHasNFO(g, mediaType) {
		return false
	}
	meta, ok := readWorkNFOMeta(g, mediaType)
	if !ok || strings.TrimSpace(meta.TMDBID) == "" {
		return false
	}
	return meta.Premiered == ""
}

// backfillWorkDates 只查询一次 TMDB 详情并把缺失的日期补进已有 NFO，
// 用于「已刮削、只差日期」的作品，避免整部作品重新刮削。
func (s *Service) backfillWorkDates(ctx context.Context, client *tmdb.Client, g workGroup, mediaType string) error {
	if client == nil {
		return nil
	}
	meta, ok := readWorkNFOMeta(g, mediaType)
	if !ok || strings.TrimSpace(meta.TMDBID) == "" {
		return nil
	}
	raw, err := client.Lookup(ctx, meta.TMDBID, mediaType)
	if err != nil {
		return err
	}
	info, err := decodeTMDBInfo(raw, mediaType)
	if err != nil {
		return err
	}
	nfo, _ := workMetaPaths(g, mediaType)
	_, err = patchNFODates(nfo, mediaType, info.ReleaseDate, info.EndDate)
	return err
}

// writeWorkNFOWithDates 写入完整 NFO，并带上发行日期 / 完结日期。
func writeWorkNFOWithDates(root, path, title, tmdbID, plot string, year *int, mediaType, releaseDate, endDate string, actors []nfoActor) error {
	nfo := workNFO{
		XMLName: xml.Name{Local: root},
		Title:   strings.TrimSpace(title),
		TMDBID:  strings.TrimSpace(tmdbID),
		Plot:    strings.TrimSpace(plot),
		Actors:  actors,
	}
	if year != nil && *year > 0 {
		nfo.Year = strconv.Itoa(*year)
	}
	nfo.Premiered = normalizeDateValue(releaseDate)
	if mediaType == MediaTypeTV {
		nfo.EndDate = normalizeDateValue(endDate)
	}
	return writeXML(path, nfo)
}

func nfoTagRe(tag string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>(.*?)</` + tag + `\s*>`)
}

// patchNFODates 在已有 NFO 上只补写缺失的日期节点，已有值（可能是用户手写）不动。
// 返回是否改动了文件。
func patchNFODates(path string, mediaType, releaseDate, endDate string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lower := strings.ToLower(string(data))
	if !strings.Contains(lower, "<movie") && !strings.Contains(lower, "<tvshow") {
		return false, nil
	}
	fields := [][2]string{{"premiered", normalizeDateValue(releaseDate)}}
	if mediaType == MediaTypeTV {
		fields = append(fields, [2]string{"enddate", normalizeDateValue(endDate)})
	}
	changed := false
	for _, field := range fields {
		tag, value := field[0], field[1]
		if value == "" {
			continue
		}
		re := nfoTagRe(tag)
		if loc := re.FindSubmatchIndex(data); loc != nil {
			if strings.TrimSpace(string(data[loc[2]:loc[3]])) != "" {
				continue
			}
			next := make([]byte, 0, len(data)+len(value))
			next = append(next, data[:loc[2]]...)
			next = append(next, value...)
			next = append(next, data[loc[3]:]...)
			data = next
			changed = true
			continue
		}
		updated, ok := insertNFONode(data, tag, value)
		if ok {
			data = updated
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, data, 0o644)
}

// insertNFONode 在最后一个 movie / tvshow 根节点结束前插入一个简单节点。
func insertNFONode(data []byte, tag, value string) ([]byte, bool) {
	return insertNFORaw(data, "<"+tag+">"+value+"</"+tag+">")
}

// insertNFORaw 在根节点结束前插入一段已经拼好的节点文本。
func insertNFORaw(data []byte, node string) ([]byte, bool) {
	matches := nfoRootCloseRe.FindAllIndex(data, -1)
	if len(matches) == 0 {
		return data, false
	}
	idx := matches[len(matches)-1][0]
	fragment := "  " + node + "\n"
	updated := make([]byte, 0, len(data)+len(fragment))
	updated = append(updated, data[:idx]...)
	updated = append(updated, fragment...)
	updated = append(updated, data[idx:]...)
	return updated, true
}
