package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"litepan/internal/domain"
)

// 光鸭分享收藏（buei 定制）：独立仓储，不修改上游 Store 结构体。
type guangyaShareLinkRepo struct{ db *DB }

// NewGuangyaShareLinkRepo 构造光鸭分享收藏仓储。
func NewGuangyaShareLinkRepo(db *DB) domain.GuangyaShareLinkRepository {
	return &guangyaShareLinkRepo{db: db}
}

const guangyaShareLinkColumns = `id, share_id, url, password, title, owner, created_at, updated_at, last_used_at`

func (r *guangyaShareLinkRepo) List(ctx context.Context) ([]*domain.GuangyaShareLink, error) {
	rows, err := r.db.read.QueryContext(ctx,
		`SELECT `+guangyaShareLinkColumns+`
		 FROM buei_guangya_share_links
		 ORDER BY last_used_at DESC, id DESC`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	links := make([]*domain.GuangyaShareLink, 0, 16)
	for rows.Next() {
		link, scanErr := scanGuangyaShareLink(rows)
		if scanErr != nil {
			return nil, wrapDB(scanErr)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return links, nil
}

func (r *guangyaShareLinkRepo) Upsert(ctx context.Context, link *domain.GuangyaShareLink) (*domain.GuangyaShareLink, error) {
	if link == nil {
		return nil, domain.Errorf(domain.CodeValidation, "收藏记录不能为空")
	}
	if link.ShareID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "分享 ID 不能为空")
	}
	now := time.Now().UTC()
	createdAt := link.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := now
	if link.UpdatedAt.After(updatedAt) {
		updatedAt = link.UpdatedAt.UTC()
	}
	lastUsedAt := link.LastUsedAt
	if lastUsedAt.IsZero() {
		lastUsedAt = now
	}

	if _, err := r.db.write.ExecContext(ctx,
		`INSERT INTO buei_guangya_share_links
		   (share_id, url, password, title, owner, created_at, updated_at, last_used_at)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT(share_id) DO UPDATE SET
		   url=excluded.url,
		   password=excluded.password,
		   title=excluded.title,
		   owner=excluded.owner,
		   updated_at=excluded.updated_at,
		   last_used_at=excluded.last_used_at`,
		link.ShareID, link.URL, link.Password, link.Title, link.Owner,
		tsValue(createdAt), tsValue(updatedAt), tsValue(lastUsedAt),
	); err != nil {
		return nil, wrapDB(err)
	}

	// 用写连接回读，确保同一事务/连接可见。
	row := r.db.write.QueryRowContext(ctx,
		`SELECT `+guangyaShareLinkColumns+`
		 FROM buei_guangya_share_links WHERE share_id=?`, link.ShareID)
	saved, err := scanGuangyaShareLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	if err != nil {
		return nil, wrapDB(err)
	}
	return saved, nil
}

func (r *guangyaShareLinkRepo) Delete(ctx context.Context, id int64) error {
	result, err := r.db.write.ExecContext(ctx, `DELETE FROM buei_guangya_share_links WHERE id=?`, id)
	if err != nil {
		return wrapDB(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return wrapDB(err)
	}
	if affected == 0 {
		return domain.Errf(domain.CodeNotFound)
	}
	return nil
}

type guangyaShareLinkScanner interface {
	Scan(dest ...any) error
}

func scanGuangyaShareLink(scanner guangyaShareLinkScanner) (*domain.GuangyaShareLink, error) {
	var (
		link      domain.GuangyaShareLink
		createdAt sql.NullString
		updatedAt sql.NullString
		lastUsed  sql.NullString
	)
	if err := scanner.Scan(
		&link.ID, &link.ShareID, &link.URL, &link.Password, &link.Title, &link.Owner,
		&createdAt, &updatedAt, &lastUsed,
	); err != nil {
		return nil, err
	}
	link.CreatedAt = parseTS(createdAt)
	link.UpdatedAt = parseTS(updatedAt)
	link.LastUsedAt = parseTS(lastUsed)
	return &link, nil
}
