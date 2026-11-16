package guangyashare

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

const shareTokenCacheTTL = 30 * time.Minute

// Options 构造光鸭分享收藏服务。
type Options struct {
	Exec     *driverexec.Executor
	Accounts domain.AccountRepository
	Links    domain.GuangyaShareLinkRepository
	Log      *slog.Logger
}

type cachedShareToken struct {
	token   string
	expires time.Time
}

// Service 负责分享解析与转存编排；公开解析不依赖任何账号凭证。
type Service struct {
	exec     *driverexec.Executor
	accounts domain.AccountRepository
	links    domain.GuangyaShareLinkRepository
	log      *slog.Logger
	client   *shareClient

	tokenMu sync.Mutex
	tokens  map[string]cachedShareToken
}

// ShareItem 是分享内的一个文件或目录。
type ShareItem struct {
	ID       string    `json:"id"`
	ParentID string    `json:"parent_id"`
	Name     string    `json:"name"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	FileType int       `json:"file_type"`
	Kind     string    `json:"kind"`
	Path     string    `json:"path"`
	Depth    int       `json:"depth"`
}

// ParseInput 是解析分享链接的入参。
type ParseInput struct {
	URL      string
	Password string
}

// ParseResult 是分享解析结果，只包含分享根目录内容，子目录按需通过 List 加载。
type ParseResult struct {
	ShareID      string      `json:"share_id"`
	Title        string      `json:"title"`
	Owner        string      `json:"owner"`
	NeedCode     bool        `json:"need_code"`
	NeedPassword bool        `json:"need_password"`
	Items        []ShareItem `json:"items"`
}

// ListInput 是列出分享内某个目录的入参。
type ListInput struct {
	URL        string
	ShareID    string
	Password   string
	ParentID   string
	ParentPath string
}

// ListResult 是目录列表结果。
type ListResult struct {
	Items []ShareItem `json:"items"`
}

// TransferInput 是转存入参。
type TransferInput struct {
	URL       string
	ShareID   string
	Password  string
	FileIDs   []string
	AccountID int64
	ParentID  string
}

// TransferResult 是转存结果。
type TransferResult struct {
	TaskID    string `json:"task_id"`
	Completed bool   `json:"completed"`
}

// New 创建服务。
func New(opts Options) *Service {
	logger := opts.Log
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		exec:     opts.Exec,
		accounts: opts.Accounts,
		links:    opts.Links,
		log:      logger,
		client:   newShareClient(),
		tokens:   make(map[string]cachedShareToken),
	}
}

// Parse 解析分享链接并列出分享根目录内容。
func (s *Service) Parse(ctx context.Context, in ParseInput) (*ParseResult, error) {
	if s == nil || s.client == nil {
		return nil, domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	link, err := ParseShareLink(in.URL)
	if err != nil {
		return nil, err
	}
	password := strings.TrimSpace(in.Password)
	if password == "" {
		password = link.Password
	}

	summary, err := s.client.shareSummary(ctx, link.ShareID)
	if err != nil {
		return nil, err
	}
	result := &ParseResult{
		ShareID:  link.ShareID,
		Title:    strings.TrimSpace(summary.Title),
		Owner:    strings.TrimSpace(summary.NickName),
		NeedCode: summary.NeedCode,
	}
	if summary.NeedCode && password == "" {
		result.NeedPassword = true
		return result, nil
	}
	if !summary.NeedCode {
		// 无提取码的分享不携带口令，避免用户误填的口令触发 209。
		password = ""
	}

	items, err := s.listItems(ctx, link.ShareID, password, "", "")
	if err != nil {
		return nil, err
	}
	result.Items = items
	s.saveLink(ctx, link, password, summary, in.URL)
	return result, nil
}

// SavedLinks 返回已收藏的分享链接，按最近使用时间倒序。
func (s *Service) SavedLinks(ctx context.Context) ([]*domain.GuangyaShareLink, error) {
	if s == nil || s.links == nil {
		return []*domain.GuangyaShareLink{}, nil
	}
	return s.links.List(ctx)
}

// DeleteSavedLink 删除一条收藏记录；不影响网盘文件与分享本身。
func (s *Service) DeleteSavedLink(ctx context.Context, id int64) error {
	if s == nil || s.links == nil {
		return domain.Errorf(domain.CodeNotImplement, "收藏功能未启用")
	}
	return s.links.Delete(ctx, id)
}

// List 列出分享内指定目录的直接内容。
func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	if s == nil || s.client == nil {
		return nil, domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	shareID, password, err := resolveShareInput(in.URL, in.ShareID, in.Password)
	if err != nil {
		return nil, err
	}
	items, err := s.listItems(ctx, shareID, password, strings.TrimSpace(in.ParentID), normalizeParentPath(in.ParentPath))
	if err != nil {
		return nil, err
	}
	return &ListResult{Items: items}, nil
}

// Transfer 把分享内选中的文件转存到指定光鸭账号目录。
func (s *Service) Transfer(ctx context.Context, in TransferInput) (*TransferResult, error) {
	if s == nil {
		return nil, domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	shareID, password, err := resolveShareInput(in.URL, in.ShareID, in.Password)
	if err != nil {
		return nil, err
	}
	fileIDs := normalizeFileIDs(in.FileIDs)
	if len(fileIDs) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择要转存的文件")
	}
	if in.AccountID <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择转存账号")
	}
	if s.exec == nil || s.accounts == nil {
		return nil, domain.Errorf(domain.CodeInternal, "转存服务未初始化")
	}

	account, err := s.accounts.Get(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	if account.DriverType != "guangya" {
		return nil, domain.Errorf(domain.CodeValidation, "只能转存到光鸭云盘账号")
	}
	if !account.IsActive {
		return nil, domain.Errorf(domain.CodeValidation, "目标账号已停用")
	}

	summary, err := s.client.shareSummary(ctx, shareID)
	if err != nil {
		return nil, err
	}
	if !summary.NeedCode {
		// 分享已移除提取码时忽略前端残留的口令。
		password = ""
	}
	accessToken, err := s.accessToken(ctx, shareID, password, false)
	if err != nil {
		return nil, err
	}

	var (
		taskID    string
		completed bool
	)
	err = s.exec.Run(ctx, in.AccountID, func(drv driver.Driver) error {
		restorer, ok := drv.(driver.GuangyaShareRestorer)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持光鸭分享转存")
		}
		var runErr error
		taskID, completed, runErr = restorer.RestoreShare(ctx, driver.GuangyaShareRestoreRequest{
			AccessToken: accessToken,
			FileIDs:     fileIDs,
			ParentID:    strings.TrimSpace(in.ParentID),
		})
		return runErr
	})
	if err != nil {
		return nil, err
	}
	if !completed {
		s.log.Info("光鸭分享转存任务等待超时，任务可能仍在执行", "account_id", in.AccountID, "task_id", taskID, "count", len(fileIDs))
	}
	return &TransferResult{TaskID: taskID, Completed: completed}, nil
}

func (s *Service) saveLink(ctx context.Context, link LinkInfo, password string, summary *shareSummaryData, rawURL string) {
	if s.links == nil {
		return
	}
	now := time.Now().UTC()
	if _, err := s.links.Upsert(ctx, &domain.GuangyaShareLink{
		ShareID:    link.ShareID,
		URL:        strings.TrimSpace(rawURL),
		Password:   password,
		Title:      strings.TrimSpace(summary.Title),
		Owner:      strings.TrimSpace(summary.NickName),
		CreatedAt:  now,
		UpdatedAt:  now,
		LastUsedAt: now,
	}); err != nil {
		s.log.Warn("保存光鸭分享收藏失败", "share_id", link.ShareID, "error", err)
	}
}

func (s *Service) listItems(ctx context.Context, shareID, password, parentID, parentPath string) ([]ShareItem, error) {
	token, err := s.accessToken(ctx, shareID, password, false)
	if err != nil {
		return nil, err
	}
	entries, err := s.client.listShareFiles(ctx, token, parentID)
	if err != nil {
		if !isShareAccessTokenExpired(err) {
			return nil, err
		}
		token, err = s.accessToken(ctx, shareID, password, true)
		if err != nil {
			return nil, err
		}
		entries, err = s.client.listShareFiles(ctx, token, parentID)
		if err != nil {
			return nil, err
		}
	}

	depth := strings.Count(parentPath, "/")
	items := make([]ShareItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, toShareItem(entry, parentID, parentPath, depth))
	}
	return items, nil
}

func (s *Service) accessToken(ctx context.Context, shareID, password string, force bool) (string, error) {
	key := shareTokenCacheKey(shareID, password)
	if !force {
		if token, ok := s.cachedToken(key); ok {
			return token, nil
		}
	}
	token, err := s.client.shareAccessToken(ctx, shareID, password)
	if err != nil {
		return "", err
	}
	s.storeToken(key, token)
	return token, nil
}

func (s *Service) cachedToken(key string) (string, bool) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	entry, ok := s.tokens[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expires) {
		delete(s.tokens, key)
		return "", false
	}
	return entry.token, true
}

func (s *Service) storeToken(key, token string) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.tokens == nil {
		s.tokens = make(map[string]cachedShareToken)
	}
	if len(s.tokens) >= 128 {
		now := time.Now()
		for k, entry := range s.tokens {
			if now.After(entry.expires) {
				delete(s.tokens, k)
			}
		}
	}
	s.tokens[key] = cachedShareToken{token: token, expires: time.Now().Add(shareTokenCacheTTL)}
}

func shareTokenCacheKey(shareID, password string) string {
	sum := sha256.Sum256([]byte(shareID + "\x00" + password))
	return fmt.Sprintf("%x", sum)
}

func isShareAccessTokenExpired(err error) bool {
	return err != nil && strings.Contains(err.Error(), "访问令牌")
}

func resolveShareInput(rawURL, shareID, password string) (string, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	shareID = strings.TrimSpace(shareID)
	password = strings.TrimSpace(password)
	if rawURL != "" {
		link, err := ParseShareLink(rawURL)
		if err != nil {
			return "", "", err
		}
		if shareID == "" {
			shareID = link.ShareID
		}
		if password == "" {
			password = link.Password
		}
	}
	if shareID == "" {
		return "", "", domain.Errorf(domain.CodeValidation, "分享 ID 为空，请重新解析分享链接")
	}
	return shareID, password, nil
}

func normalizeParentPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	return "/" + path
}

func toShareItem(entry shareEntry, parentID, parentPath string, depth int) ShareItem {
	name := strings.TrimSpace(entry.FileName)
	if name == "" {
		name = entry.FileID
	}
	itemPath := "/" + name
	if parentPath != "" {
		itemPath = strings.TrimRight(parentPath, "/") + "/" + name
	}
	modTime := time.Time{}
	if entry.UTime > 0 {
		modTime = time.Unix(entry.UTime, 0)
	} else if entry.CTime > 0 {
		modTime = time.Unix(entry.CTime, 0)
	}
	isDir := entry.ResType == 2
	return ShareItem{
		ID: entry.FileID,
		// 分享列表接口不保证返回 parentId，列表请求的父目录才是可靠来源。
		ParentID: parentID,
		Name:     name,
		IsDir:    isDir,
		Size:     entry.FileSize,
		ModTime:  modTime,
		FileType: entry.FileType,
		Kind:     classifyKind(name, entry.FileType, isDir),
		Path:     itemPath,
		Depth:    depth,
	}
}

func normalizeFileIDs(fileIDs []string) []string {
	out := make([]string, 0, len(fileIDs))
	seen := make(map[string]struct{}, len(fileIDs))
	for _, id := range fileIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
