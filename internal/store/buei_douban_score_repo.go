package store

import (
	"context"
	"database/sql"
	"time"

	"litepan/internal/domain"
)

// 豆瓣评分仓储（buei 定制）：独立仓储，不修改上游 Store 结构体。

type bueiDoubanScoreRepo struct{ db *DB }

// NewBueiDoubanScoreRepo 构造豆瓣评分仓储。
func NewBueiDoubanScoreRepo(db *DB) domain.BueiDoubanScoreRepository {
	return &bueiDoubanScoreRepo{db: db}
}

func (r *bueiDoubanScoreRepo) ListByTask(ctx context.Context, strmTaskID int64) ([]*domain.BueiDoubanScore, error) {
	rows, err := r.db.read.QueryContext(ctx, `
SELECT strm_task_id, item_id, title, year, media_type, query_title, query_year,
       douban_id, douban_title, douban_year, rating, votes, is_tv, status, message,
       manual_douban_id, manual_rating, no_auto,
       checked_at, updated_at
FROM buei_douban_scores
WHERE strm_task_id = ?
ORDER BY item_id`, strmTaskID)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	out := make([]*domain.BueiDoubanScore, 0, 128)
	for rows.Next() {
		s, err := scanBueiDoubanScore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return out, nil
}

func (r *bueiDoubanScoreRepo) Upsert(ctx context.Context, scores []*domain.BueiDoubanScore) error {
	if len(scores) == 0 {
		return nil
	}
	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return wrapDB(err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, s := range scores {
		if s == nil || s.StrmTaskID <= 0 || s.ItemID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO buei_douban_scores (
  strm_task_id, item_id, title, year, media_type, query_title, query_year,
  douban_id, douban_title, douban_year, rating, votes, is_tv, status, message,
  checked_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(strm_task_id, item_id) DO UPDATE SET
  title=excluded.title,
  year=excluded.year,
  media_type=excluded.media_type,
  query_title=excluded.query_title,
  query_year=excluded.query_year,
  douban_id=excluded.douban_id,
  douban_title=excluded.douban_title,
  douban_year=excluded.douban_year,
  rating=excluded.rating,
  votes=excluded.votes,
  is_tv=excluded.is_tv,
  status=excluded.status,
  message=excluded.message,
  checked_at=excluded.checked_at,
  updated_at=excluded.updated_at`,
			s.StrmTaskID, s.ItemID, s.Title, intOrNull(s.Year), s.MediaType,
			s.QueryTitle, intOrNull(s.QueryYear),
			s.DoubanID, s.DoubanTitle, intOrNull(s.DoubanYear),
			floatOrNull(s.Rating), s.Votes, boolToInt(s.IsTV), s.Status, s.Message,
			tsValue(s.CheckedAt), tsValue(s.UpdatedAt)); err != nil {
			return wrapDB(err)
		}
	}
	return wrapDB(tx.Commit())
}

func (r *bueiDoubanScoreRepo) Counts(ctx context.Context) (map[int64]domain.BueiDoubanScoreCounts, error) {
	rows, err := r.db.read.QueryContext(ctx, `
SELECT strm_task_id, COUNT(*),
       COALESCE(SUM(CASE WHEN rating IS NOT NULL OR manual_rating IS NOT NULL THEN 1 ELSE 0 END), 0)
FROM buei_douban_scores
GROUP BY strm_task_id`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	out := make(map[int64]domain.BueiDoubanScoreCounts, 8)
	for rows.Next() {
		var (
			taskID int64
			counts domain.BueiDoubanScoreCounts
		)
		if err := rows.Scan(&taskID, &counts.Total, &counts.Scored); err != nil {
			return nil, wrapDB(err)
		}
		out[taskID] = counts
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return out, nil
}

func (r *bueiDoubanScoreRepo) DeleteByTask(ctx context.Context, strmTaskID int64) error {
	_, err := r.db.write.ExecContext(ctx, `DELETE FROM buei_douban_scores WHERE strm_task_id = ?`, strmTaskID)
	return wrapDB(err)
}

// SaveOverride 只写三个手动覆盖列：已有记录保留查询结果，没有记录时新建一行。
func (r *bueiDoubanScoreRepo) SaveOverride(ctx context.Context, strmTaskID int64, itemID string, o domain.BueiDoubanScoreOverride) error {
	if strmTaskID <= 0 || itemID == "" {
		return domain.Errorf(domain.CodeValidation, "豆瓣评分覆盖项缺少任务或项目 ID")
	}
	_, err := r.db.write.ExecContext(ctx, `
INSERT INTO buei_douban_scores (strm_task_id, item_id, updated_at, manual_douban_id, manual_rating, no_auto)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(strm_task_id, item_id) DO UPDATE SET
  manual_douban_id=excluded.manual_douban_id,
  manual_rating=excluded.manual_rating,
  no_auto=excluded.no_auto,
  updated_at=excluded.updated_at`,
		strmTaskID, itemID, tsValue(time.Now()),
		o.ManualDoubanID, floatOrNull(o.ManualRating), boolToInt(o.NoAutoQuery))
	return wrapDB(err)
}

type bueiDoubanScoreScanner interface {
	Scan(dest ...any) error
}

func scanBueiDoubanScore(row bueiDoubanScoreScanner) (*domain.BueiDoubanScore, error) {
	var (
		s          domain.BueiDoubanScore
		year       sql.NullInt64
		queryYear  sql.NullInt64
		doubanYr   sql.NullInt64
		rating     sql.NullFloat64
		manualRate sql.NullFloat64
		isTV       int
		noAuto     int
		checkedAt  sql.NullString
		updatedAt  sql.NullString
	)
	if err := row.Scan(
		&s.StrmTaskID, &s.ItemID, &s.Title, &year, &s.MediaType, &s.QueryTitle, &queryYear,
		&s.DoubanID, &s.DoubanTitle, &doubanYr, &rating, &s.Votes, &isTV, &s.Status, &s.Message,
		&s.ManualDoubanID, &manualRate, &noAuto,
		&checkedAt, &updatedAt,
	); err != nil {
		return nil, wrapDB(err)
	}
	s.Year = intPtrFromNull(year)
	s.QueryYear = intPtrFromNull(queryYear)
	s.DoubanYear = intPtrFromNull(doubanYr)
	if rating.Valid {
		v := rating.Float64
		s.Rating = &v
	}
	if manualRate.Valid {
		v := manualRate.Float64
		s.ManualRating = &v
	}
	s.IsTV = isTV != 0
	s.NoAutoQuery = noAuto != 0
	s.CheckedAt = parseTS(checkedAt)
	s.UpdatedAt = parseTS(updatedAt)
	return &s, nil
}

func intPtrFromNull(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func intOrNull(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func floatOrNull(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
