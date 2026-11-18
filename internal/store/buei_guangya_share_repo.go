package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"litepan/internal/domain"
)

// 光鸭分享收藏（buei 定制）：独立仓储，不修改上游 Store 结构体。
type guangyaShareLinkRepo struct{ db *DB }

// NewGuangyaShareLinkRepo 构造光鸭分享收藏仓储。
func NewGuangyaShareLinkRepo(db *DB) domain.GuangyaShareLinkRepository {
	return &guangyaShareLinkRepo{db: db}
}

const guangyaShareLinkColumns = `id, share_id, url, password, title, owner, created_at, updated_at, last_used_at,
	last_account_id, last_parent_id, last_base_path, last_base_dirs, last_title, last_season, last_is_series,
	last_browse_path, rename_replace_enabled, rename_replace_find, rename_replace_with, auto_strm,
	auto_scrape, auto_scrape_task_id, auto_emby, emby_config_id, emby_mode, emby_library_id`

func (r *guangyaShareLinkRepo) List(ctx context.Context) ([]*domain.GuangyaShareLink, error) {
	rows, err := r.db.read.QueryContext(ctx,
		`SELECT `+guangyaShareLinkColumns+`
		 FROM buei_guangya_share_links
		 ORDER BY id ASC`)
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

func (r *guangyaShareLinkRepo) UpdateDestination(ctx context.Context, shareID string, dest *domain.GuangyaShareDestination) error {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return domain.Errorf(domain.CodeValidation, "分享 ID 不能为空")
	}
	var (
		accountID    int64
		parentID     string
		basePath     string
		baseDirs     = []byte("[]")
		title        string
		season       = 1
		isSeries     = 1
		autoStrm     = 1
		autoScrape   = 1
		scrapeTaskID int64
		refreshEmby  = 1
		embyConfigID string
		embyMode     = "global"
		embyLibrary  string
	)
	if dest != nil {
		accountID = dest.AccountID
		parentID = dest.ParentID
		basePath = dest.BasePath
		dirs := dest.BaseDirs
		if dirs == nil {
			dirs = []string{}
		}
		if data, err := json.Marshal(dirs); err == nil {
			baseDirs = data
		}
		title = dest.Title
		season = dest.Season
		isSeries = boolToInt(dest.IsSeries)
		autoStrm = boolToInt(dest.AutoStrm)
		autoScrape = boolToInt(dest.AutoScrape)
		scrapeTaskID = dest.ScrapeTaskID
		refreshEmby = boolToInt(dest.RefreshEmby)
		embyConfigID = strings.TrimSpace(dest.EmbyConfigID)
		embyMode = strings.TrimSpace(dest.EmbyMode)
		if embyMode == "" {
			embyMode = "global"
		}
		embyLibrary = strings.TrimSpace(dest.EmbyLibraryID)
	}
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_guangya_share_links
		 SET last_account_id=?, last_parent_id=?, last_base_path=?, last_base_dirs=?, last_title=?,
		     last_season=?, last_is_series=?, auto_strm=?, auto_scrape=?, auto_scrape_task_id=?,
		     auto_emby=?, emby_config_id=?, emby_mode=?, emby_library_id=?, updated_at=?
		 WHERE share_id=?`,
		accountID, parentID, basePath, string(baseDirs), title, season, isSeries, autoStrm,
		autoScrape, scrapeTaskID, refreshEmby, embyConfigID, embyMode, embyLibrary,
		tsValue(time.Now().UTC()), shareID,
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

func (r *guangyaShareLinkRepo) UpdateBrowsePath(ctx context.Context, shareID string, crumbs []domain.GuangyaShareBrowseCrumb) error {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return domain.Errorf(domain.CodeValidation, "分享 ID 不能为空")
	}
	if crumbs == nil {
		crumbs = []domain.GuangyaShareBrowseCrumb{}
	}
	data, err := json.Marshal(crumbs)
	if err != nil {
		return wrapDB(err)
	}
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_guangya_share_links SET last_browse_path=?, updated_at=? WHERE share_id=?`,
		string(data), tsValue(time.Now().UTC()), shareID,
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

func (r *guangyaShareLinkRepo) UpdateRenameReplace(ctx context.Context, shareID string, rule domain.GuangyaShareRenameReplace) error {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return domain.Errorf(domain.CodeValidation, "分享 ID 不能为空")
	}
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_guangya_share_links
		 SET rename_replace_enabled=?, rename_replace_find=?, rename_replace_with=?, updated_at=?
		 WHERE share_id=?`,
		boolToInt(rule.Enabled), rule.Find, rule.ReplaceWith, tsValue(time.Now().UTC()), shareID,
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

func (r *guangyaShareLinkRepo) Delete(ctx context.Context, id int64) (string, error) {
	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return "", wrapDB(err)
	}
	defer func() { _ = tx.Rollback() }()

	var shareID string
	if err := tx.QueryRowContext(ctx,
		`SELECT share_id FROM buei_guangya_share_links WHERE id=?`, id,
	).Scan(&shareID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.Errf(domain.CodeNotFound)
		}
		return "", wrapDB(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM buei_guangya_share_links WHERE id=?`, id); err != nil {
		return "", wrapDB(err)
	}
	if err := tx.Commit(); err != nil {
		return "", wrapDB(err)
	}
	return shareID, nil
}

type guangyaShareLinkScanner interface {
	Scan(dest ...any) error
}

func normalizeGuangyaEmbyMode(raw string) string {
	if strings.TrimSpace(raw) == "library" {
		return "library"
	}
	return "global"
}

func scanGuangyaShareLink(scanner guangyaShareLinkScanner) (*domain.GuangyaShareLink, error) {
	var (
		link           domain.GuangyaShareLink
		createdAt      sql.NullString
		updatedAt      sql.NullString
		lastUsed       sql.NullString
		lastAccountID  int64
		lastParentID   string
		lastBasePath   string
		lastBaseDirs   string
		lastTitle      string
		lastSeason     int
		lastIsSeries   int
		lastBrowsePath string
		replaceEnabled int
		replaceFind    string
		replaceWith    string
		autoStrm       int
		autoScrape     int
		scrapeTaskID   int64
		autoEmby       int
		embyConfigID   string
		embyMode       string
		embyLibraryID  string
	)
	if err := scanner.Scan(
		&link.ID, &link.ShareID, &link.URL, &link.Password, &link.Title, &link.Owner,
		&createdAt, &updatedAt, &lastUsed,
		&lastAccountID, &lastParentID, &lastBasePath, &lastBaseDirs,
		&lastTitle, &lastSeason, &lastIsSeries, &lastBrowsePath,
		&replaceEnabled, &replaceFind, &replaceWith, &autoStrm, &autoScrape, &scrapeTaskID,
		&autoEmby, &embyConfigID, &embyMode, &embyLibraryID,
	); err != nil {
		return nil, err
	}
	link.CreatedAt = parseTS(createdAt)
	link.UpdatedAt = parseTS(updatedAt)
	link.LastUsedAt = parseTS(lastUsed)
	if lastAccountID > 0 {
		dest := &domain.GuangyaShareDestination{
			AccountID:     lastAccountID,
			ParentID:      lastParentID,
			BasePath:      lastBasePath,
			Title:         lastTitle,
			Season:        lastSeason,
			IsSeries:      lastIsSeries != 0,
			AutoStrm:      autoStrm != 0,
			AutoScrape:    autoScrape != 0,
			ScrapeTaskID:  scrapeTaskID,
			RefreshEmby:   autoEmby != 0,
			EmbyConfigID:  embyConfigID,
			EmbyMode:      normalizeGuangyaEmbyMode(embyMode),
			EmbyLibraryID: embyLibraryID,
		}
		if strings.TrimSpace(lastBaseDirs) != "" {
			_ = json.Unmarshal([]byte(lastBaseDirs), &dest.BaseDirs)
		}
		link.LastDestination = dest
	}
	if strings.TrimSpace(lastBrowsePath) != "" {
		_ = json.Unmarshal([]byte(lastBrowsePath), &link.LastBrowsePath)
	}
	link.RenameReplace = domain.GuangyaShareRenameReplace{
		Enabled:     replaceEnabled != 0,
		Find:        replaceFind,
		ReplaceWith: replaceWith,
	}
	return &link, nil
}
