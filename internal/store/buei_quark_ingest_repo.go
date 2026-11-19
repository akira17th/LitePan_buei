package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"litepan/internal/domain"
)

// 夸克入库收藏（buei 定制）：独立仓储，不修改上游 Store 结构体。
type quarkIngestFavoriteRepo struct{ db *DB }

// NewQuarkIngestFavoriteRepo 构造夸克入库收藏仓储。
func NewQuarkIngestFavoriteRepo(db *DB) domain.QuarkIngestFavoriteRepository {
	return &quarkIngestFavoriteRepo{db: db}
}

const quarkIngestFavoriteColumns = `id, account_id, link, folder_id, parent_folder_id, folder_name, created_at, updated_at`

func (r *quarkIngestFavoriteRepo) ListByAccount(ctx context.Context, accountID int64) ([]*domain.QuarkIngestFavorite, error) {
	rows, err := r.db.read.QueryContext(ctx,
		`SELECT `+quarkIngestFavoriteColumns+`
		 FROM buei_quark_ingest_favorites
		 WHERE account_id=?
		 ORDER BY id ASC`, accountID)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	favorites := make([]*domain.QuarkIngestFavorite, 0, 8)
	for rows.Next() {
		favorite, scanErr := scanQuarkIngestFavorite(rows)
		if scanErr != nil {
			return nil, wrapDB(scanErr)
		}
		favorites = append(favorites, favorite)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return favorites, nil
}

func (r *quarkIngestFavoriteRepo) Create(ctx context.Context, favorite *domain.QuarkIngestFavorite) (*domain.QuarkIngestFavorite, error) {
	if favorite == nil {
		return nil, domain.Errorf(domain.CodeValidation, "收藏记录不能为空")
	}
	now := time.Now().UTC()
	createdAt := favorite.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := favorite.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	result, err := r.db.write.ExecContext(ctx,
		`INSERT INTO buei_quark_ingest_favorites
		   (account_id, link, folder_id, parent_folder_id, folder_name, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		favorite.AccountID, strings.TrimSpace(favorite.Link), strings.TrimSpace(favorite.FolderID),
		strings.TrimSpace(favorite.ParentFolderID), strings.TrimSpace(favorite.FolderName),
		tsValue(createdAt), tsValue(updatedAt),
	)
	if err != nil {
		return nil, wrapDB(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, wrapDB(err)
	}
	return r.getByID(ctx, r.db.write, id)
}

func (r *quarkIngestFavoriteRepo) Update(ctx context.Context, favorite *domain.QuarkIngestFavorite) (*domain.QuarkIngestFavorite, error) {
	if favorite == nil {
		return nil, domain.Errorf(domain.CodeValidation, "收藏记录不能为空")
	}
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_quark_ingest_favorites
		 SET link=?, folder_id=?, parent_folder_id=?, folder_name=?, updated_at=?
		 WHERE id=? AND account_id=?`,
		strings.TrimSpace(favorite.Link), strings.TrimSpace(favorite.FolderID),
		strings.TrimSpace(favorite.ParentFolderID), strings.TrimSpace(favorite.FolderName),
		tsValue(time.Now().UTC()),
		favorite.ID, favorite.AccountID,
	)
	if err != nil {
		return nil, wrapDB(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, wrapDB(err)
	}
	if affected == 0 {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	return r.getByID(ctx, r.db.write, favorite.ID)
}

func (r *quarkIngestFavoriteRepo) Delete(ctx context.Context, id, accountID int64) error {
	result, err := r.db.write.ExecContext(ctx,
		`DELETE FROM buei_quark_ingest_favorites WHERE id=? AND account_id=?`, id, accountID)
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

func (r *quarkIngestFavoriteRepo) UpdateFolderName(ctx context.Context, id int64, name string) error {
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_quark_ingest_favorites
		 SET folder_name=?, updated_at=?
		 WHERE id=?`,
		strings.TrimSpace(name), tsValue(time.Now().UTC()), id,
	)
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

func (r *quarkIngestFavoriteRepo) getByID(ctx context.Context, q *sql.DB, id int64) (*domain.QuarkIngestFavorite, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+quarkIngestFavoriteColumns+`
		 FROM buei_quark_ingest_favorites WHERE id=?`, id)
	favorite, err := scanQuarkIngestFavorite(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	if err != nil {
		return nil, wrapDB(err)
	}
	return favorite, nil
}

func scanQuarkIngestFavorite(row rowScanner) (*domain.QuarkIngestFavorite, error) {
	var (
		favorite  domain.QuarkIngestFavorite
		createdAt sql.NullString
		updatedAt sql.NullString
	)
	if err := row.Scan(
		&favorite.ID,
		&favorite.AccountID,
		&favorite.Link,
		&favorite.FolderID,
		&favorite.ParentFolderID,
		&favorite.FolderName,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	favorite.CreatedAt = parseTS(createdAt)
	favorite.UpdatedAt = parseTS(updatedAt)
	return &favorite, nil
}
