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
	last_browse_path, rename_replace_enabled, rename_replace_find, rename_replace_with, rename_replace_rules, auto_strm,
	auto_scrape, auto_scrape_task_id, auto_emby, emby_config_id, emby_mode, emby_library_id,
	update_weekday, update_hour, update_minute`

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

func (r *guangyaShareLinkRepo) Get(ctx context.Context, id int64) (*domain.GuangyaShareLink, error) {
	if id <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	row := r.db.read.QueryRowContext(ctx,
		`SELECT `+guangyaShareLinkColumns+`
		 FROM buei_guangya_share_links WHERE id=?`, id)
	saved, err := scanGuangyaShareLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	if err != nil {
		return nil, wrapDB(err)
	}
	return saved, nil
}

func (r *guangyaShareLinkRepo) Create(ctx context.Context, link *domain.GuangyaShareLink) (*domain.GuangyaShareLink, error) {
	if link == nil {
		return nil, domain.Errorf(domain.CodeValidation, "收藏记录不能为空")
	}
	shareID := strings.TrimSpace(link.ShareID)
	if shareID == "" {
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
	result, err := r.db.write.ExecContext(ctx,
		`INSERT INTO buei_guangya_share_links
		   (share_id, url, password, title, owner, created_at, updated_at, last_used_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		shareID, strings.TrimSpace(link.URL), link.Password, strings.TrimSpace(link.Title),
		strings.TrimSpace(link.Owner), tsValue(createdAt), tsValue(updatedAt), tsValue(lastUsedAt),
	)
	if err != nil {
		return nil, wrapDB(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, wrapDB(err)
	}
	return r.Get(ctx, id)
}

func (r *guangyaShareLinkRepo) Touch(ctx context.Context, id int64) error {
	if id <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	now := tsValue(time.Now().UTC())
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_guangya_share_links SET last_used_at=?, updated_at=? WHERE id=?`,
		now, now, id,
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

func (r *guangyaShareLinkRepo) UpdateIdentity(ctx context.Context, id int64, shareID, url, password, title, owner string, clearBrowsePath bool) (*domain.GuangyaShareLink, error) {
	if id <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "分享 ID 不能为空")
	}
	now := tsValue(time.Now().UTC())
	browsePathSQL := "last_browse_path"
	if clearBrowsePath {
		browsePathSQL = "'[]'"
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer func() { _ = tx.Rollback() }()

	// 分享 ID 变化时清理该收藏旧的转存/丢弃记录，避免历史与新分享错配。
	if clearBrowsePath {
		if _, err := tx.ExecContext(ctx, `DELETE FROM buei_guangya_share_transferred_files WHERE link_id=?`, id); err != nil {
			return nil, wrapDB(err)
		}
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE buei_guangya_share_links
		 SET share_id=?, url=?, password=?, title=?, owner=?, updated_at=?, last_used_at=?,
		     last_browse_path=`+browsePathSQL+`
		 WHERE id=?`,
		shareID, strings.TrimSpace(url), password, strings.TrimSpace(title), strings.TrimSpace(owner),
		now, now, id,
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
	if err := tx.Commit(); err != nil {
		return nil, wrapDB(err)
	}
	row := r.db.write.QueryRowContext(ctx,
		`SELECT `+guangyaShareLinkColumns+`
		 FROM buei_guangya_share_links WHERE id=?`, id)
	saved, err := scanGuangyaShareLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	if err != nil {
		return nil, wrapDB(err)
	}
	return saved, nil
}

func (r *guangyaShareLinkRepo) UpdateDestination(ctx context.Context, id int64, dest *domain.GuangyaShareDestination) error {
	if id <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
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
		 WHERE id=?`,
		accountID, parentID, basePath, string(baseDirs), title, season, isSeries, autoStrm,
		autoScrape, scrapeTaskID, refreshEmby, embyConfigID, embyMode, embyLibrary,
		tsValue(time.Now().UTC()), id,
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

func (r *guangyaShareLinkRepo) UpdateBrowsePath(ctx context.Context, id int64, crumbs []domain.GuangyaShareBrowseCrumb) error {
	if id <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	if crumbs == nil {
		crumbs = []domain.GuangyaShareBrowseCrumb{}
	}
	data, err := json.Marshal(crumbs)
	if err != nil {
		return wrapDB(err)
	}
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_guangya_share_links SET last_browse_path=?, updated_at=? WHERE id=?`,
		string(data), tsValue(time.Now().UTC()), id,
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

func (r *guangyaShareLinkRepo) UpdateRenameReplace(ctx context.Context, id int64, rule domain.GuangyaShareRenameReplace) error {
	if id <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	rules := rule.NormalizeRules()
	first := domain.GuangyaShareRenameRule{}
	if len(rules) > 0 {
		first = rules[0]
	}
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_guangya_share_links
		 SET rename_replace_enabled=?, rename_replace_find=?, rename_replace_with=?,
		     rename_replace_rules=?, updated_at=?
		 WHERE id=?`,
		boolToInt(rule.Enabled), first.Find, first.ReplaceWith, string(rulesJSON), tsValue(time.Now().UTC()), id,
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

func (r *guangyaShareLinkRepo) UpdateSchedule(ctx context.Context, id int64, schedule domain.IngestUpdateSchedule) error {
	if id <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
	schedule, err := schedule.Normalize()
	if err != nil {
		return err
	}
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_guangya_share_links
		 SET update_weekday=?, update_hour=?, update_minute=?, updated_at=?
		 WHERE id=?`,
		schedule.Weekday, schedule.Hour, schedule.Minute, tsValue(time.Now().UTC()), id,
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

func (r *guangyaShareLinkRepo) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return domain.Errorf(domain.CodeValidation, "收藏 ID 不能为空")
	}
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
		replaceRules   string
		autoStrm       int
		autoScrape     int
		scrapeTaskID   int64
		autoEmby       int
		embyConfigID   string
		embyMode       string
		embyLibraryID  string
		updateWeekday  int
		updateHour     int
		updateMinute   int
	)
	if err := scanner.Scan(
		&link.ID, &link.ShareID, &link.URL, &link.Password, &link.Title, &link.Owner,
		&createdAt, &updatedAt, &lastUsed,
		&lastAccountID, &lastParentID, &lastBasePath, &lastBaseDirs,
		&lastTitle, &lastSeason, &lastIsSeries, &lastBrowsePath,
		&replaceEnabled, &replaceFind, &replaceWith, &replaceRules, &autoStrm, &autoScrape, &scrapeTaskID,
		&autoEmby, &embyConfigID, &embyMode, &embyLibraryID,
		&updateWeekday, &updateHour, &updateMinute,
	); err != nil {
		return nil, err
	}
	link.CreatedAt = parseTS(createdAt)
	link.UpdatedAt = parseTS(updatedAt)
	link.LastUsedAt = parseTS(lastUsed)
	link.UpdateSchedule = domain.IngestUpdateSchedule{Weekday: updateWeekday, Hour: updateHour, Minute: updateMinute}
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
	if strings.TrimSpace(replaceRules) != "" && strings.TrimSpace(replaceRules) != "[]" {
		var rules []domain.GuangyaShareRenameRule
		if err := json.Unmarshal([]byte(replaceRules), &rules); err == nil {
			link.RenameReplace.Rules = rules
		}
	}
	return &link, nil
}
