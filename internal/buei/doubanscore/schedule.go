package doubanscore

import (
	"time"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

// refreshMonths 是「作品上线/完结后还要继续刷新评分」的窗口：
// 距发行日期（电影）或完结日期（剧集）不超过 1 个月的作品，每天都重新查一次。
const refreshMonths = 1

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

// needsQuery 判断某部作品这次是否需要查询豆瓣：
//   - 没有评分：每次都要查；
//   - 有评分但参照日期未知（电影没有上映日期 / 剧集未完结或没有完结日期）：每次都要查；
//   - 有评分、参照日期在 1 个月内：仍要查（评分还在波动）；
//   - 有评分、参照日期已超过 1 个月：不再查。
func needsQuery(score *domain.BueiDoubanScore, item strmscrape.Item, now time.Time) bool {
	if score == nil || score.Rating == nil {
		return true
	}
	anchor := itemAnchorDate(item)
	if anchor.IsZero() {
		return true
	}
	return now.Before(anchor.AddDate(0, refreshMonths, 0))
}
