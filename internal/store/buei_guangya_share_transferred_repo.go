package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"litepan/internal/domain"
)

// 光鸭分享转存历史（buei 定制）：记录已转存文件的新旧名称，独立仓储。
type guangyaShareTransferredRepo struct{ db *DB }

// NewGuangyaShareTransferredRepo 构造光鸭分享转存历史仓储。
func NewGuangyaShareTransferredRepo(db *DB) domain.GuangyaShareTransferredRepository {
	return &guangyaShareTransferredRepo{db: db}
}

func (r *guangyaShareTransferredRepo) ListByLink(ctx context.Context, linkID int64) ([]*domain.GuangyaShareTransferredFile, error) {
	if linkID <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	rows, err := r.db.read.QueryContext(ctx,
		`SELECT file_id, original_name, new_name, account_id, target_parent_id, status, updated_at
		 FROM buei_guangya_share_transferred_files
		 WHERE link_id=?
		 ORDER BY updated_at DESC, id DESC`, linkID)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	files := make([]*domain.GuangyaShareTransferredFile, 0, 32)
	for rows.Next() {
		var (
			file    domain.GuangyaShareTransferredFile
			updated sql.NullString
		)
		if err := rows.Scan(
			&file.FileID, &file.OriginalName, &file.NewName,
			&file.AccountID, &file.TargetParentID, &file.Status, &updated,
		); err != nil {
			return nil, wrapDB(err)
		}
		file.UpdatedAt = parseTS(updated)
		files = append(files, &file)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return files, nil
}

func (r *guangyaShareTransferredRepo) UpsertMany(ctx context.Context, linkID int64, files []*domain.GuangyaShareTransferredFile) error {
	if linkID <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	if len(files) == 0 {
		return nil
	}
	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return wrapDB(err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	for _, file := range files {
		if file == nil || strings.TrimSpace(file.FileID) == "" {
			continue
		}
		updatedAt := file.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = now
		}
		status := strings.TrimSpace(file.Status)
		if status == "" {
			status = domain.GuangyaShareFileTransferred
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO buei_guangya_share_transferred_files
			   (link_id, file_id, account_id, original_name, new_name, target_parent_id, status, created_at, updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?)
			 ON CONFLICT(link_id, file_id) DO UPDATE SET
			   account_id=excluded.account_id,
			   original_name=excluded.original_name,
			   new_name=excluded.new_name,
			   target_parent_id=excluded.target_parent_id,
			   status=excluded.status,
			   updated_at=excluded.updated_at`,
			linkID, file.FileID, file.AccountID, file.OriginalName, file.NewName,
			file.TargetParentID, status, tsValue(now), tsValue(updatedAt),
		); err != nil {
			return wrapDB(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapDB(err)
	}
	return nil
}

func (r *guangyaShareTransferredRepo) DeleteByLink(ctx context.Context, linkID int64) error {
	if linkID <= 0 {
		return nil
	}
	if _, err := r.db.write.ExecContext(ctx,
		`DELETE FROM buei_guangya_share_transferred_files WHERE link_id=?`, linkID,
	); err != nil {
		return wrapDB(err)
	}
	return nil
}
