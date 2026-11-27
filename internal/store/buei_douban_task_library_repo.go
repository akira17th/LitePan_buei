package store

import (
	"context"
	"database/sql"
	"time"

	"litepan/internal/domain"
)

// buei:douban-score —— STRM 任务 ↔ Emby 媒体库关联仓储（豆瓣评分查到后刷对应媒体库）。

type bueiDoubanTaskLibraryRepo struct{ db *DB }

// NewBueiDoubanTaskLibraryRepo 构造任务媒体库关联仓储。
func NewBueiDoubanTaskLibraryRepo(db *DB) domain.BueiDoubanTaskLibraryRepository {
	return &bueiDoubanTaskLibraryRepo{db: db}
}

func (r *bueiDoubanTaskLibraryRepo) Map(ctx context.Context) (map[int64]domain.BueiDoubanTaskLibrary, error) {
	rows, err := r.db.read.QueryContext(ctx, `
SELECT strm_task_id, library_id, library_name, updated_at
FROM buei_douban_task_libraries`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	out := make(map[int64]domain.BueiDoubanTaskLibrary, 8)
	for rows.Next() {
		var (
			taskID    int64
			lib       domain.BueiDoubanTaskLibrary
			updatedAt sql.NullString
		)
		if err := rows.Scan(&taskID, &lib.LibraryID, &lib.LibraryName, &updatedAt); err != nil {
			return nil, wrapDB(err)
		}
		lib.UpdatedAt = parseTS(updatedAt)
		out[taskID] = lib
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return out, nil
}

func (r *bueiDoubanTaskLibraryRepo) Set(ctx context.Context, strmTaskID int64, libraryID, libraryName string) error {
	if strmTaskID <= 0 {
		return wrapDB(domain.Errorf(domain.CodeValidation, "strm_task_id 无效"))
	}
	_, err := r.db.write.ExecContext(ctx, `
INSERT INTO buei_douban_task_libraries (strm_task_id, library_id, library_name, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(strm_task_id) DO UPDATE SET
  library_id=excluded.library_id,
  library_name=excluded.library_name,
  updated_at=excluded.updated_at`,
		strmTaskID, libraryID, libraryName, tsValue(time.Now().UTC()))
	return wrapDB(err)
}

func (r *bueiDoubanTaskLibraryRepo) Delete(ctx context.Context, strmTaskID int64) error {
	_, err := r.db.write.ExecContext(ctx, `DELETE FROM buei_douban_task_libraries WHERE strm_task_id = ?`, strmTaskID)
	return wrapDB(err)
}
