package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"litepan/internal/domain"
)

// Emby 媒体库最近同步时间（buei 定制）：独立仓储，不修改上游 Store 结构体。

type bueiEmbyLibrarySyncRepo struct{ db *DB }

// NewBueiEmbyLibrarySyncRepo 构造最近同步时间仓储。
func NewBueiEmbyLibrarySyncRepo(db *DB) domain.BueiEmbyLibrarySyncRepository {
	return &bueiEmbyLibrarySyncRepo{db: db}
}

func (r *bueiEmbyLibrarySyncRepo) List(ctx context.Context) ([]*domain.BueiEmbyLibrarySync, error) {
	rows, err := r.db.read.QueryContext(ctx,
		`SELECT library_id, library_name, last_synced_at FROM buei_emby_library_syncs`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	out := make([]*domain.BueiEmbyLibrarySync, 0, 8)
	for rows.Next() {
		var (
			item domain.BueiEmbyLibrarySync
			ts   sql.NullString
		)
		if err := rows.Scan(&item.LibraryID, &item.LibraryName, &ts); err != nil {
			return nil, wrapDB(err)
		}
		item.LastSyncedAt = parseTS(ts)
		out = append(out, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return out, nil
}

func (r *bueiEmbyLibrarySyncRepo) Get(ctx context.Context, libraryID string) (*domain.BueiEmbyLibrarySync, error) {
	var (
		item domain.BueiEmbyLibrarySync
		ts   sql.NullString
	)
	err := r.db.read.QueryRowContext(ctx,
		`SELECT library_id, library_name, last_synced_at FROM buei_emby_library_syncs WHERE library_id=?`,
		strings.TrimSpace(libraryID)).Scan(&item.LibraryID, &item.LibraryName, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	if err != nil {
		return nil, wrapDB(err)
	}
	item.LastSyncedAt = parseTS(ts)
	return &item, nil
}

func (r *bueiEmbyLibrarySyncRepo) Save(ctx context.Context, item *domain.BueiEmbyLibrarySync) error {
	if item == nil || strings.TrimSpace(item.LibraryID) == "" {
		return domain.Errorf(domain.CodeValidation, "媒体库 ID 不能为空")
	}
	now := time.Now().UTC()
	_, err := r.db.write.ExecContext(ctx,
		`INSERT INTO buei_emby_library_syncs(library_id, library_name, last_synced_at, updated_at)
		 VALUES(?,?,?,?)
		 ON CONFLICT(library_id) DO UPDATE SET
		     library_name = excluded.library_name,
		     last_synced_at = excluded.last_synced_at,
		     updated_at = excluded.updated_at`,
		strings.TrimSpace(item.LibraryID), strings.TrimSpace(item.LibraryName), tsValue(item.LastSyncedAt), tsValue(now))
	return wrapDB(err)
}
