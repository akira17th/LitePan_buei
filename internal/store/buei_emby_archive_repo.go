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

// Emby 自动归档配置与执行日志（buei 定制）：独立仓储，不修改上游 Store 结构体。

type bueiEmbyArchiveRuleRepo struct{ db *DB }

// NewBueiEmbyArchiveRuleRepo 构造自动归档配置仓储。
func NewBueiEmbyArchiveRuleRepo(db *DB) domain.BueiEmbyArchiveRuleRepository {
	return &bueiEmbyArchiveRuleRepo{db: db}
}

const bueiEmbyArchiveRuleColumns = `id, archive_library_id, archive_library_name, config_id, source_libraries, interval_hours, enabled, last_run_at, next_run_at, created_at, updated_at`

func (r *bueiEmbyArchiveRuleRepo) List(ctx context.Context) ([]*domain.BueiEmbyArchiveRule, error) {
	rows, err := r.db.read.QueryContext(ctx,
		`SELECT `+bueiEmbyArchiveRuleColumns+`
		 FROM buei_emby_archive_rules
		 ORDER BY id ASC`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	out := make([]*domain.BueiEmbyArchiveRule, 0, 4)
	for rows.Next() {
		rule, scanErr := scanBueiEmbyArchiveRule(rows)
		if scanErr != nil {
			return nil, wrapDB(scanErr)
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return out, nil
}

func (r *bueiEmbyArchiveRuleRepo) Get(ctx context.Context, archiveLibraryID string) (*domain.BueiEmbyArchiveRule, error) {
	row := r.db.read.QueryRowContext(ctx,
		`SELECT `+bueiEmbyArchiveRuleColumns+`
		 FROM buei_emby_archive_rules WHERE archive_library_id=?`,
		strings.TrimSpace(archiveLibraryID))
	rule, err := scanBueiEmbyArchiveRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodeNotFound)
	}
	if err != nil {
		return nil, wrapDB(err)
	}
	return rule, nil
}

func (r *bueiEmbyArchiveRuleRepo) Save(ctx context.Context, rule *domain.BueiEmbyArchiveRule) (*domain.BueiEmbyArchiveRule, error) {
	if rule == nil {
		return nil, domain.Errorf(domain.CodeValidation, "自动归档配置不能为空")
	}
	now := time.Now().UTC()
	sources, err := json.Marshal(rule.SourceLibraries)
	if err != nil {
		return nil, domain.Wrap(domain.CodeValidation, err)
	}
	createdAt := rule.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	_, err = r.db.write.ExecContext(ctx,
		`INSERT INTO buei_emby_archive_rules
		   (archive_library_id, archive_library_name, config_id, source_libraries, interval_hours,
		    enabled, last_run_at, next_run_at, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(archive_library_id) DO UPDATE SET
		   archive_library_name=excluded.archive_library_name,
		   config_id=excluded.config_id,
		   source_libraries=excluded.source_libraries,
		   interval_hours=excluded.interval_hours,
		   enabled=excluded.enabled,
		   next_run_at=excluded.next_run_at,
		   updated_at=excluded.updated_at`,
		strings.TrimSpace(rule.ArchiveLibraryID), strings.TrimSpace(rule.ArchiveLibraryName),
		strings.TrimSpace(rule.ConfigID), string(sources), normalizeBueiArchiveInterval(rule.IntervalHours),
		bueiBoolInt(rule.Enabled),
		bueiTSValueNullable(rule.LastRunAt), bueiTSValueNullable(rule.NextRunAt),
		tsValue(createdAt), tsValue(now),
	)
	if err != nil {
		return nil, wrapDB(err)
	}
	return r.Get(ctx, rule.ArchiveLibraryID)
}

func (r *bueiEmbyArchiveRuleRepo) UpdateSchedule(ctx context.Context, archiveLibraryID string, lastRunAt, nextRunAt time.Time) error {
	result, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_emby_archive_rules
		 SET last_run_at=?, next_run_at=?, updated_at=?
		 WHERE archive_library_id=?`,
		bueiTSValueNullable(lastRunAt), bueiTSValueNullable(nextRunAt), tsValue(time.Now().UTC()),
		strings.TrimSpace(archiveLibraryID),
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

func scanBueiEmbyArchiveRule(row rowScanner) (*domain.BueiEmbyArchiveRule, error) {
	var (
		rule       domain.BueiEmbyArchiveRule
		sources    string
		lastRunAt  sql.NullString
		nextRunAt  sql.NullString
		createdAt  sql.NullString
		updatedAt  sql.NullString
		interval   int
		enabled    int
		configID   string
		archiveLib string
		libraryID  string
	)
	if err := row.Scan(
		&rule.ID,
		&libraryID,
		&archiveLib,
		&configID,
		&sources,
		&interval,
		&enabled,
		&lastRunAt,
		&nextRunAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	rule.ArchiveLibraryID = libraryID
	rule.ArchiveLibraryName = archiveLib
	rule.ConfigID = configID
	rule.IntervalHours = normalizeBueiArchiveInterval(interval)
	rule.Enabled = enabled != 0
	if strings.TrimSpace(sources) != "" {
		if err := json.Unmarshal([]byte(sources), &rule.SourceLibraries); err != nil {
			return nil, err
		}
	}
	rule.LastRunAt = parseTS(lastRunAt)
	rule.NextRunAt = parseTS(nextRunAt)
	rule.CreatedAt = parseTS(createdAt)
	rule.UpdatedAt = parseTS(updatedAt)
	return &rule, nil
}

func bueiBoolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func normalizeBueiArchiveInterval(hours int) int {
	switch hours {
	case 6, 24:
		return hours
	default:
		return 1
	}
}

func bueiTSValueNullable(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return tsValue(value)
}

type bueiEmbyArchiveRunRepo struct{ db *DB }

// NewBueiEmbyArchiveRunRepo 构造自动归档执行日志仓储。
func NewBueiEmbyArchiveRunRepo(db *DB) domain.BueiEmbyArchiveRunRepository {
	return &bueiEmbyArchiveRunRepo{db: db}
}

const bueiEmbyArchiveRunColumns = `id, archive_library_id, archive_library_name, trigger, status, started_at, finished_at, scanned, archived, skipped, failed, message, items`

func (r *bueiEmbyArchiveRunRepo) PruneByTrigger(ctx context.Context, archiveLibraryID, trigger string, keepID int64) error {
	_, err := r.db.write.ExecContext(ctx,
		`DELETE FROM buei_emby_archive_runs
		 WHERE archive_library_id=? AND trigger=? AND id<>?`,
		strings.TrimSpace(archiveLibraryID), strings.TrimSpace(trigger), keepID)
	return wrapDB(err)
}

func (r *bueiEmbyArchiveRunRepo) Create(ctx context.Context, run *domain.BueiEmbyArchiveRun) (*domain.BueiEmbyArchiveRun, error) {
	if run == nil {
		return nil, domain.Errorf(domain.CodeValidation, "执行记录不能为空")
	}
	startedAt := run.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	items, err := json.Marshal(run.Items)
	if err != nil {
		return nil, domain.Wrap(domain.CodeValidation, err)
	}
	result, err := r.db.write.ExecContext(ctx,
		`INSERT INTO buei_emby_archive_runs
		   (archive_library_id, archive_library_name, trigger, status, started_at, scanned, archived, skipped, failed, message, items)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(run.ArchiveLibraryID), strings.TrimSpace(run.ArchiveLibraryName),
		strings.TrimSpace(run.Trigger), strings.TrimSpace(run.Status), tsValue(startedAt),
		run.Scanned, run.Archived, run.Skipped, run.Failed, strings.TrimSpace(run.Message), string(items),
	)
	if err != nil {
		return nil, wrapDB(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, wrapDB(err)
	}
	run.ID = id
	run.StartedAt = startedAt
	return run, nil
}

func (r *bueiEmbyArchiveRunRepo) Finish(ctx context.Context, run *domain.BueiEmbyArchiveRun) error {
	if run == nil || run.ID <= 0 {
		return domain.Errorf(domain.CodeValidation, "执行记录不存在")
	}
	finishedAt := run.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	items, err := json.Marshal(run.Items)
	if err != nil {
		return domain.Wrap(domain.CodeValidation, err)
	}
	if _, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_emby_archive_runs
		 SET status=?, finished_at=?, scanned=?, archived=?, skipped=?, failed=?, message=?, items=?
		 WHERE id=?`,
		strings.TrimSpace(run.Status), tsValue(finishedAt), run.Scanned, run.Archived, run.Skipped,
		run.Failed, strings.TrimSpace(run.Message), string(items), run.ID,
	); err != nil {
		return wrapDB(err)
	}
	run.FinishedAt = finishedAt
	return nil
}

func (r *bueiEmbyArchiveRunRepo) ListByLibrary(ctx context.Context, archiveLibraryID string, limit int) ([]*domain.BueiEmbyArchiveRun, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.db.read.QueryContext(ctx,
		`SELECT `+bueiEmbyArchiveRunColumns+`
		 FROM buei_emby_archive_runs
		 WHERE archive_library_id=?
		 ORDER BY id DESC
		 LIMIT ?`,
		strings.TrimSpace(archiveLibraryID), limit)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()

	out := make([]*domain.BueiEmbyArchiveRun, 0, limit)
	for rows.Next() {
		run, scanErr := scanBueiEmbyArchiveRun(rows)
		if scanErr != nil {
			return nil, wrapDB(scanErr)
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(err)
	}
	return out, nil
}

func (r *bueiEmbyArchiveRunRepo) FailInterrupted(ctx context.Context, archiveLibraryID string) error {
	_, err := r.db.write.ExecContext(ctx,
		`UPDATE buei_emby_archive_runs
		 SET status=?, finished_at=?, message=?
		 WHERE archive_library_id=? AND status=?`,
		domain.BueiArchiveRunFailed, tsValue(time.Now().UTC()), "执行被应用重启中断",
		strings.TrimSpace(archiveLibraryID), domain.BueiArchiveRunRunning,
	)
	if err != nil {
		return wrapDB(err)
	}
	return nil
}

func scanBueiEmbyArchiveRun(row rowScanner) (*domain.BueiEmbyArchiveRun, error) {
	var (
		run        domain.BueiEmbyArchiveRun
		libraryID  string
		library    string
		finishedAt sql.NullString
		startedAt  sql.NullString
		items      string
	)
	if err := row.Scan(
		&run.ID,
		&libraryID,
		&library,
		&run.Trigger,
		&run.Status,
		&startedAt,
		&finishedAt,
		&run.Scanned,
		&run.Archived,
		&run.Skipped,
		&run.Failed,
		&run.Message,
		&items,
	); err != nil {
		return nil, err
	}
	run.ArchiveLibraryID = libraryID
	run.ArchiveLibraryName = library
	run.StartedAt = parseTS(startedAt)
	run.FinishedAt = parseTS(finishedAt)
	if strings.TrimSpace(items) != "" {
		if err := json.Unmarshal([]byte(items), &run.Items); err != nil {
			return nil, err
		}
	}
	return &run, nil
}
