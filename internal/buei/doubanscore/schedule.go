package doubanscore

import (
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// refreshMonths 是「作品上线/完结后还要继续刷新评分」的窗口：
// 距发行日期（电影）或完结日期（剧集）不超过 1 个月的作品，每天都重新查一次。
const refreshMonths = 1

// settleMonths 是「不再追查评分」的冷静期：上映/完结超过 3 个月的作品，
// 如果**成功查过一次**仍然没有评分（豆瓣长期无评分 / 没有条目），就不再查了——
// 再查基本也是空手而归，白耗每日 08:00 的请求预算。
// 注意：从没查过（旧影片刚入库、索引新建）或上次查询失败（风控/超时/网络）的不适用——
// 那种情况一次都没拿到豆瓣的可靠答复，必须继续查。
const settleMonths = 3

const dateOnlyLayout = "2006-01-02"

// scheduleDue 判断每天 ScheduleHour 点的自动查询是否到点。
func scheduleDue(now, lastRun time.Time) bool {
	return !now.Before(nextRunAt(now, lastRun))
}

// nextRunAt 返回下一次自动查询时间：当天 ScheduleHour 点若还没执行过，否则次日同一时间。
func nextRunAt(now, lastRun time.Time) time.Time {
	today := time.Date(now.Year(), now.Month(), now.Day(), ScheduleHour, 0, 0, 0, now.Location())
	if lastRun.IsZero() {
		if now.Before(today) {
			return today
		}
		// 从未执行且已过点：立即可执行。
		return now
	}
	if lastRun.In(now.Location()).Before(today) {
		return today
	}
	return today.AddDate(0, 0, 1)
}

// itemAnchorDate 返回评分是否需要继续刷新的参照日期：
// 电影用发行日期（上映），剧集用完结日期。日期未知返回零值。
func itemAnchorDate(item strmscrape.Item) time.Time {
	raw := item.ReleaseDate
	if item.MediaType == strmscrape.MediaTypeTV {
		raw = item.EndDate
	}
	if raw == "" {
		return time.Time{}
	}
	d, err := time.Parse(dateOnlyLayout, raw)
	if err != nil {
		return time.Time{}
	}
	return d
}

// queryDecision 是单个作品的查询判定：查不查，以及给用户看的原因。
type queryDecision struct {
	Query  bool
	Reason string
}

// decideQuery 判断某部作品这次是否需要查询豆瓣：
//   - 手动设为「不再查询」：不查（手动「查询 / 重查」仍然有效）；
//   - 手动设置了评分：不查（避免和家人手填的分数冲）；
//   - 没刮削出 TMDB ID（NFO / 文件名都没有）且没手动指定豆瓣 ID：不查；
//   - 没有评分、参照日期未知（电影没有上映日期 / 剧集未完结或没有完结日期）：每次都要查；
//   - 没有评分、参照日期在 3 个月内：仍要查（刚上映/完结，评分可能随时出现）；
//   - 没有评分、参照日期已超过 3 个月且成功查过：不再追查（豆瓣长期没有就是没有了）；
//   - 没有评分、参照日期已超过 3 个月但从没成功查过（包括上次失败）：仍要查；
//   - 有评分、参照日期在 1 个月内：仍要查（评分还在波动）；
//   - 有评分、参照日期已超过 1 个月：不再查。
func decideQuery(score *domain.BueiDoubanScore, item strmscrape.Item, now time.Time) queryDecision {
	if score != nil && score.NoAutoQuery {
		return queryDecision{Query: false, Reason: "已设为不再自动查询"}
	}
	if score != nil && score.ManualRating != nil {
		return queryDecision{Query: false, Reason: "已手动设置评分"}
	}
	manualID := ""
	if score != nil {
		manualID = strings.TrimSpace(score.ManualDoubanID)
	}
	if strings.TrimSpace(item.TMDBID) == "" && manualID == "" {
		return queryDecision{Query: false, Reason: "未刮削 TMDB，跳过"}
	}
	noRating := score == nil || score.Rating == nil
	anchor := itemAnchorDate(item)
	if anchor.IsZero() {
		// 日期未知：没有评分要继续查；已有评分也继续（可能刚完结、评分还在变动）。
		return queryDecision{Query: true, Reason: "上映/完结日期未知，继续跟踪"}
	}
	if now.After(anchor.AddDate(0, settleMonths, 0)) {
		// 上映/完结超过 3 个月：已经有评分的不再刷；成功查过仍没评分的也不再追——
		// 但一次都没查过（刚入库的旧片）或上次查询失败的要继续查。
		if noRating {
			if queriedWithoutRating(score) {
				return queryDecision{Query: false, Reason: "上映/完结超过 3 个月且查过无评分，不再查询"}
			}
			return queryDecision{Query: true, Reason: "上映/完结超过 3 个月，还没成功查过评分"}
		}
		return queryDecision{Query: false, Reason: "已有评分且上映/完结超过 1 个月"}
	}
	if noRating {
		return queryDecision{Query: true, Reason: "还没有豆瓣评分"}
	}
	if now.Before(anchor.AddDate(0, refreshMonths, 0)) {
		return queryDecision{Query: true, Reason: "上映/完结不满 1 个月，评分可能变动"}
	}
	return queryDecision{Query: false, Reason: "已有评分且上映/完结超过 1 个月"}
}

// queriedWithoutRating 判断「确实成功查过一次、但豆瓣没有给评分」：
// 只有这种情况才在 3 个月冷静期后停止追查。
// 从没查过（记录不存在 / 没有任何查询结果状态）或上次查询失败（error）都算没查过。
func queriedWithoutRating(score *domain.BueiDoubanScore) bool {
	if score == nil || score.Rating != nil {
		return false
	}
	switch score.Status {
	case domain.BueiDoubanScoreMatched, domain.BueiDoubanScoreNotFound:
		return true
	default:
		return false
	}
}

// needsQuery 判断某部作品这次是否需要查询豆瓣。
func needsQuery(score *domain.BueiDoubanScore, item strmscrape.Item, now time.Time) bool {
	return decideQuery(score, item, now).Query
}
