package guangyashare

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// buei 定制：磁力链接收藏。识别 magnet 链接后调用光鸭原生离线下载解析，
// 任务完成后的文件与光鸭分享一样展示、勾选并走同一条入库（转存）流程。
// 磁力收藏复用 guangya_share_links 存储：ShareID = "magnet:<infohash>"，URL = 完整磁力。

const (
	// magnetShareIDPrefix 是磁力收藏在 share_id 字段里的标识前缀。
	magnetShareIDPrefix = "magnet:"
	// magnetOfflineDirName 是磁力离线任务在光鸭账号内的落盘根目录（账号根目录下），
	// 每个磁力在其下按 btih 哈希建独立子目录，避免不同磁力的文件混在一起。
	magnetOfflineDirName = "来自：云添加"
	// magnetResolveTimeout 是等待光鸭离线任务完成的总超时。
	magnetResolveTimeout = 90 * time.Second
)

// magnetResolveInterval 是轮询离线任务状态的间隔；var 便于测试缩短等待。
var magnetResolveInterval = 1500 * time.Millisecond

// fakeMagnetMove 记录一次移动（测试断言用）。
type fakeMagnetMove struct {
	fileIDs        []string
	targetParentID string
	sourceParentID string
}

// ParseMagnetInput 是磁力解析入参。
type ParseMagnetInput struct {
	URL string
	// SavedID 大于 0 表示打开已有磁力收藏（直接重新列出离线目录内容）。
	SavedID int64
	// AccountID 是执行离线下载的光鸭账号；为 0 时取第一个可用光鸭账号。
	AccountID int64
}

// IsMagnetLink 判断输入是否是磁力链接。
func IsMagnetLink(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(input)), "magnet:?xt=urn:btih:")
}

// isMagnetShareID 判断 share_id 是否是磁力收藏标识。
func isMagnetShareID(shareID string) bool {
	return strings.HasPrefix(strings.TrimSpace(shareID), magnetShareIDPrefix)
}

// magnetHashFromShareID 从磁力 share_id 中提取 btih 哈希。
func magnetHashFromShareID(shareID string) string {
	return strings.TrimPrefix(strings.TrimSpace(shareID), magnetShareIDPrefix)
}

// magnetAccountConfigKey 记录某磁力离线时使用的光鸭账号，后续浏览/删除直接定位该账号。
func magnetAccountConfigKey(shareID string) string {
	return "buei_guangya_magnet_account_" + magnetHashFromShareID(shareID)
}

// rememberMagnetAccount 记录磁力离线使用的光鸭账号；失败只记日志。
func (s *Service) rememberMagnetAccount(ctx context.Context, shareID string, accountID int64) {
	if s.configs == nil || accountID <= 0 {
		return
	}
	if err := s.configs.Set(ctx, magnetAccountConfigKey(shareID), strconv.FormatInt(accountID, 10)); err != nil {
		s.logger().Warn("记录磁力离线账号失败", "share_id", shareID, "error", err)
	}
}

// loadMagnetAccount 读取磁力离线使用的光鸭账号；没有记录时返回 0。
func (s *Service) loadMagnetAccount(ctx context.Context, shareID string) int64 {
	if s.configs == nil {
		return 0
	}
	raw, ok, err := s.configs.Get(ctx, magnetAccountConfigKey(shareID))
	if err != nil || !ok {
		return 0
	}
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// magnetParams 解析磁力链接的参数（xt/dn 等）；非磁力链接返回 nil。
func magnetParams(rawURL string) url.Values {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(parsed.Scheme, "magnet") {
		return nil
	}
	query := parsed.RawQuery
	if query == "" {
		query = parsed.Opaque
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil
	}
	return values
}

// MagnetInfoHash 从磁力链接中提取 btih 信息哈希（大写 hex，40 位）。
// 兼容 32 位 base32 编码；无法识别时返回空串。
func MagnetInfoHash(rawURL string) string {
	values := magnetParams(rawURL)
	if values == nil {
		return ""
	}
	for _, value := range values["xt"] {
		lower := strings.ToLower(value)
		if !strings.HasPrefix(lower, "urn:btih:") {
			continue
		}
		value = value[len("urn:btih:"):]
		switch len(value) {
		case 40:
			if _, err := hex.DecodeString(value); err != nil {
				return ""
			}
			return strings.ToUpper(value)
		case 32:
			decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(value))
			if err != nil {
				return ""
			}
			return strings.ToUpper(hex.EncodeToString(decoded))
		default:
			return ""
		}
	}
	return ""
}

// magnetDisplayName 从磁力链接中取 dn 显示名，可能为空。
func magnetDisplayName(rawURL string) string {
	values := magnetParams(rawURL)
	if values == nil {
		return ""
	}
	if dn := values.Get("dn"); dn != "" {
		return strings.TrimSpace(dn)
	}
	return ""
}

// magnetOfflineDirs 是离线目录的目录段数组。
func magnetOfflineDirs() []string {
	return strings.Split(magnetOfflineDirName, "/")
}

// ParseMagnet 解析磁力链接：提交光鸭离线任务并等待完成，然后列出落盘文件。
// 流程与分享解析一致：成功后返回 ParseResult（Items 为光鸭网盘文件），前端照常勾选入库。
func (s *Service) ParseMagnet(ctx context.Context, in ParseMagnetInput) (*ParseResult, error) {
	if s == nil || s.exec == nil || s.accounts == nil {
		return nil, domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	rawURL := strings.TrimSpace(in.URL)
	if !IsMagnetLink(rawURL) {
		return nil, domain.Errorf(domain.CodeValidation, "请粘贴 magnet 磁力链接")
	}
	infoHash := MagnetInfoHash(rawURL)
	if infoHash == "" {
		return nil, domain.Errorf(domain.CodeValidation, "磁力链接缺少 btih 信息哈希")
	}

	var saved *domain.GuangyaShareLink
	if in.SavedID > 0 {
		if s.links == nil {
			return nil, domain.Errorf(domain.CodeNotImplement, "收藏功能未启用")
		}
		item, err := s.links.Get(ctx, in.SavedID)
		if err != nil {
			return nil, err
		}
		if item != nil {
			saved = item
			// 打开已有收藏时以收藏里保存的磁力为准。
			if IsMagnetLink(item.URL) {
				rawURL = item.URL
			}
		}
	}

	shareID := magnetShareIDPrefix + infoHash
	// 账号优先级：显式指定 > 上次离线记录 > 第一个启用的光鸭账号。
	// 打开已有收藏时必须落回离线时的账号，否则哈希子目录不在该账号下会被误判为空。
	accountID := in.AccountID
	if accountID <= 0 {
		accountID = s.loadMagnetAccount(ctx, shareID)
	}
	if accountID <= 0 {
		id, err := s.firstGuangyaAccountID(ctx)
		if err != nil {
			return nil, err
		}
		accountID = id
	}

	// 每个磁力固定用 btih 哈希做子目录，直读/提交都指向它。
	// 子目录里已有文件时直接列出返回，不重复提交离线任务。
	// buei: 直读时同样要确保收藏存在，否则收藏列表里不会出现该磁力。
	dirID, dirErr := s.ensureMagnetDir(ctx, accountID, infoHash)
	if dirErr != nil {
		return nil, dirErr
	}
	items, listErr := s.listMagnetFiles(ctx, accountID, dirID)
	if listErr == nil && len(items) > 0 {
		s.rememberMagnetAccount(ctx, shareID, accountID)
		favorite, favErr := s.ensureMagnetFavorite(ctx, saved, shareID, rawURL, infoHash)
		if favErr != nil {
			return nil, favErr
		}
		saved = favorite
		return s.magnetParseResult(ctx, saved, shareID, rawURL, items), nil
	}

	items, err := s.resolveMagnet(ctx, accountID, rawURL, dirID)
	if err != nil {
		// 离线失败时清掉刚建的空哈希目录，避免残留一堆空壳。
		s.removeMagnetDirIfEmpty(ctx, accountID, dirID)
		return nil, err
	}
	s.rememberMagnetAccount(ctx, shareID, accountID)

	favorite, favErr := s.ensureMagnetFavorite(ctx, saved, shareID, rawURL, infoHash)
	if favErr != nil {
		return nil, favErr
	}
	saved = favorite
	// 磁力没有 dn 时用落盘内容名作为收藏标题，便于识别。
	if saved != nil && magnetDisplayName(rawURL) == "" && len(items) > 0 {
		s.updateMagnetTitle(ctx, saved, rawURL, items[0].Name)
		saved.Title = sanitizeFolderName(items[0].Name)
	}
	return s.magnetParseResult(ctx, saved, shareID, rawURL, items), nil
}

// updateMagnetTitle 磁力解析完成后用落盘名字补充收藏标题（保留原链接身份）。
func (s *Service) updateMagnetTitle(ctx context.Context, link *domain.GuangyaShareLink, rawURL, title string) {
	title = sanitizeFolderName(title)
	if title == "" || s.links == nil || link == nil || title == link.Title {
		return
	}
	if _, err := s.links.UpdateIdentity(ctx, link.ID, link.ShareID, rawURL, link.Password, title, link.Owner, false); err != nil {
		s.logger().Warn("更新磁力收藏标题失败", "link_id", link.ID, "error", err)
	}
}

// ensureMagnetFavorite 确保磁力收藏存在：已有收藏只刷新最近使用时间，没有则新建。
func (s *Service) ensureMagnetFavorite(ctx context.Context, saved *domain.GuangyaShareLink, shareID, rawURL, infoHash string) (*domain.GuangyaShareLink, error) {
	if s.links == nil {
		return saved, nil
	}
	if saved != nil {
		if err := s.links.Touch(ctx, saved.ID); err != nil {
			s.logger().Warn("更新磁力收藏最近使用时间失败", "link_id", saved.ID, "error", err)
		}
		return saved, nil
	}
	// 收藏标题取磁力 dn；没有 dn 时先用哈希，解析完成后再用落盘内容名补充。
	title := sanitizeFolderName(magnetDisplayName(rawURL))
	if title == "" {
		title = infoHash
	}
	now := time.Now().UTC()
	created, err := s.links.Create(ctx, &domain.GuangyaShareLink{
		ShareID:   shareID,
		URL:       rawURL,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		s.logger().Warn("保存磁力收藏失败", "share_id", shareID, "error", err)
		return saved, nil
	}
	return created, nil
}

// magnetParseResult 组装与分享解析一致的返回结构。
func (s *Service) magnetParseResult(ctx context.Context, saved *domain.GuangyaShareLink, shareID, rawURL string, items []ShareItem) *ParseResult {
	title := magnetDisplayName(rawURL)
	result := &ParseResult{
		ShareID: shareID,
		Title:   title,
		Items:   items,
	}
	if saved != nil {
		result.SavedLinkID = saved.ID
		if saved.Title != "" {
			title = saved.Title
		}
		result.LastDestination = saved.LastDestination
		result.LastBrowsePath = saved.LastBrowsePath
		if saved.RenameReplace.Enabled || len(saved.RenameReplace.NormalizeRules()) > 0 {
			rule := saved.RenameReplace
			result.RenameReplace = &rule
		}
		result.TransferredFiles = s.loadTransferred(ctx, saved.ID)
	}
	result.Title = title
	result.LastBase = s.loadLastBase(ctx)
	return result
}

// firstGuangyaAccountID 取第一个启用的光鸭账号。
func (s *Service) firstGuangyaAccountID(ctx context.Context) (int64, error) {
	accounts, err := s.accounts.List(ctx)
	if err != nil {
		return 0, err
	}
	for _, account := range accounts {
		if account != nil && account.DriverType == "guangya" && account.IsActive {
			return account.ID, nil
		}
	}
	return 0, domain.Errorf(domain.CodeValidation, "没有可用的光鸭云盘账号")
}

// ensureMagnetDir 确保光鸭账号内存在「来自：云添加/<btih哈希>」目录并返回其 ID。
func (s *Service) ensureMagnetDir(ctx context.Context, accountID int64, infoHash string) (string, error) {
	segments := append(magnetOfflineDirs(), strings.TrimSpace(infoHash))
	var dirID string
	if err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		id, err := ensureTargetFolder(ctx, drv, "", segments)
		if err != nil {
			return err
		}
		dirID = id
		return nil
	}); err != nil {
		return "", err
	}
	return dirID, nil
}

// resolveMagnet 在光鸭账号内提交磁力离线任务（落到 dirID）并等待完成，返回落盘文件。
func (s *Service) resolveMagnet(ctx context.Context, accountID int64, rawURL, dirID string) ([]ShareItem, error) {
	var taskID string

	if err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		adder, ok := drv.(driver.OfflineURLDownloader)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持离线下载")
		}
		results, err := adder.AddOfflineURLs(ctx, driver.OfflineURLRequest{URLs: []string{rawURL}, ParentID: dirID})
		if err != nil {
			return err
		}
		for _, result := range results {
			if result.Success && strings.TrimSpace(result.ProviderTaskID) != "" {
				taskID = strings.TrimSpace(result.ProviderTaskID)
				return nil
			}
		}
		message := "光鸭拒绝该磁力离线任务"
		for _, result := range results {
			if strings.TrimSpace(result.Message) != "" {
				message = result.Message
				break
			}
		}
		return domain.Errorf(domain.CodeDriverError, "%s", message)
	}); err != nil {
		return nil, err
	}

	if err := s.waitMagnetTask(ctx, accountID, taskID); err != nil {
		return nil, err
	}
	items, err := s.listMagnetFiles(ctx, accountID, dirID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, domain.Errorf(domain.CodeDriverError, "磁力解析完成，但离线目录里没有文件")
	}
	return items, nil
}

// DeleteMagnetFiles 删除磁力收藏对应的「来自：云添加/<哈希>」目录（含里面全部内容）。
// 目录不存在时忽略（可能从未离线成功或已被手动删除）。
func (s *Service) DeleteMagnetFiles(ctx context.Context, shareID string) error {
	if s == nil || s.exec == nil || s.accounts == nil {
		return domain.Errorf(domain.CodeInternal, "光鸭分享服务未初始化")
	}
	infoHash := magnetHashFromShareID(shareID)
	if infoHash == "" {
		return nil
	}
	// 优先用离线时记录的账号；没有记录才取第一个启用账号兜底。
	accountID := s.loadMagnetAccount(ctx, shareID)
	if accountID <= 0 {
		id, err := s.firstGuangyaAccountID(ctx)
		if err != nil {
			return err
		}
		accountID = id
	}
	var dirID string
	if listErr := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		// 先定位「来自：云添加」目录 ID（不存在说明从未离线成功）。
		baseID, err := ensureTargetFolder(ctx, drv, "", magnetOfflineDirs())
		if err != nil {
			return err
		}
		lister, ok := drv.(driver.Lister)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持列目录")
		}
		files, err := lister.ListFiles(ctx, baseID)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.IsDir && file.Name == infoHash {
				dirID = file.ID
				break
			}
		}
		return nil
	}); listErr != nil {
		return listErr
	}
	if dirID == "" {
		return nil
	}
	if err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		deleter, ok := drv.(driver.Deleter)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持删除文件")
		}
		return deleter.DeleteFiles(ctx, []string{dirID})
	}); err != nil {
		// 目录可能已被手动删除；找不到时不阻塞收藏删除。
		if appErr, ok := domain.AsAppError(err); ok && appErr.Code == domain.CodeNotFound {
			return nil
		}
		return err
	}
	if s.invalidateDir != nil {
		s.invalidateDir(accountID, magnetOfflineDirName)
	}
	s.logger().Info("已删除磁力离线目录", "account_id", accountID, "share_id", shareID)
	return nil
}

// removeMagnetDirIfEmpty 删除空的哈希子目录（离线失败后的残留清理）；非空或删除失败时不动。
func (s *Service) removeMagnetDirIfEmpty(ctx context.Context, accountID int64, dirID string) {
	if strings.TrimSpace(dirID) == "" {
		return
	}
	var items []domain.FileItem
	err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		lister, ok := drv.(driver.Lister)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持列目录")
		}
		files, listErr := lister.ListFiles(ctx, dirID)
		if listErr != nil {
			return listErr
		}
		items = files
		return nil
	})
	if err != nil || len(items) > 0 {
		return
	}
	_ = s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		deleter, ok := drv.(driver.Deleter)
		if !ok {
			return nil
		}
		return deleter.DeleteFiles(ctx, []string{dirID})
	})
}

// waitMagnetTask 轮询光鸭离线任务直到成功或失败。
func (s *Service) waitMagnetTask(ctx context.Context, accountID int64, taskID string) error {
	deadline := time.Now().Add(magnetResolveTimeout)
	var lastMessage string
	for {
		var (
			status string
			err    error
		)
		if err = s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
			refresher, ok := drv.(driver.OfflineTaskRefresher)
			if !ok {
				return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持离线下载")
			}
			updates, refreshErr := refresher.RefreshOfflineTasks(ctx, []driver.OfflineTaskRef{{ProviderTaskID: taskID}})
			if refreshErr != nil {
				return refreshErr
			}
			for _, update := range updates {
				status = update.Status
				lastMessage = strings.TrimSpace(update.Message)
			}
			return nil
		}); err != nil {
			return err
		}
		switch status {
		case driver.OfflineStatusSuccess:
			return nil
		case driver.OfflineStatusFailed:
			if lastMessage == "" {
				lastMessage = "光鸭磁力解析失败"
			}
			return domain.Errorf(domain.CodeDriverError, "%s", lastMessage)
		}
		if time.Now().After(deadline) {
			return domain.Errorf(domain.CodeDriverError, "等待光鸭解析磁力超时，请稍后重试")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(magnetResolveInterval):
		}
	}
}

// listMagnetFiles 列出磁力离线目录中的文件，转换为与分享列表一致的 ShareItem。
func (s *Service) listMagnetFiles(ctx context.Context, accountID int64, dirID string) ([]ShareItem, error) {
	var items []ShareItem
	if err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		lister, ok := drv.(driver.Lister)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持列目录")
		}
		files, err := lister.ListFiles(ctx, dirID)
		if err != nil {
			return err
		}
		items = make([]ShareItem, 0, len(files))
		for _, file := range files {
			items = append(items, ShareItem{
				ID:      file.ID,
				Name:    file.Name,
				IsDir:   file.IsDir,
				Size:    file.Size,
				ModTime: file.ModTime,
				Kind:    classifyKind(file.Name, 0, file.IsDir),
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return items, nil
}

// transferMagnetFiles 处理磁力收藏的入库：文件已在光鸭账号内，只需移动到目标目录。
// 之后的重命名、STRM、刮削、刷库与分享转存共用同一段逻辑。
func (s *Service) transferMagnetFiles(ctx context.Context, in TransferInput, shareID string) (*TransferResult, error) {
	fileIDs := normalizeFileIDs(in.FileIDs)
	if len(fileIDs) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择要转存的文件")
	}
	if in.AccountID <= 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择转存账号")
	}
	account, err := s.accounts.Get(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	if account.DriverType != "guangya" || !account.IsActive {
		return nil, domain.Errorf(domain.CodeValidation, "只能入库到启用的光鸭云盘账号")
	}

	progressID := strings.TrimSpace(in.ProgressID)
	s.progress.start(progressID)
	asyncPending := false
	defer func() {
		if asyncPending {
			return
		}
		if err != nil {
			s.progress.finish(progressID, err.Error())
			return
		}
		s.progress.finish(progressID, "")
	}()

	targetPath, pathErr := normalizeTargetPath(in.TargetPath)
	if pathErr != nil {
		return nil, pathErr
	}

	offlineDirID, err := s.ensureMagnetDir(ctx, in.AccountID, magnetHashFromShareID(shareID))
	if err != nil {
		return nil, err
	}

	var targetParentID = strings.TrimSpace(in.ParentID)
	if err = s.exec.Run(ctx, in.AccountID, func(drv driver.Driver) error {
		mover, ok := drv.(driver.Mover)
		if !ok {
			return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持移动文件")
		}
		parentID := targetParentID
		if len(targetPath) > 0 {
			var folderErr error
			if parentID, folderErr = ensureTargetFolder(ctx, drv, targetParentID, targetPath); folderErr != nil {
				return folderErr
			}
		}
		targetParentID = parentID
		return mover.MoveFiles(ctx, fileIDs, targetParentID, offlineDirID)
	}); err != nil {
		return nil, err
	}
	if s.invalidateDir != nil {
		s.invalidateDir(in.AccountID, targetParentID)
	}

	files := normalizeTransferFiles(in.Files)
	renamePlan := buildRenamePlan(files)
	// 磁力入库是同步完成的（移动 + 重命名都在本次请求内），前端据此保留弹窗展示结果。
	result := &TransferResult{Completed: true}
	strmDirs := targetDirectorySegments(in.BaseDirs, targetPath)

	if len(renamePlan) > 0 {
		s.progress.setPhase(progressID, GuangyaSharePhaseRename)
		result.Renamed, result.RenameSkipped = s.renameAfterRestore(ctx, in.AccountID, targetParentID, renamePlan)
	}
	linkID := in.SavedID
	if linkID > 0 {
		s.saveTransferred(ctx, linkID, in.AccountID, targetParentID, files)
	}
	if in.AutoStrm {
		s.progress.setPhase(progressID, GuangyaSharePhaseStrm)
		result.StrmGenerated, result.StrmMatched, result.StrmError = s.generateStrm(ctx, in.AccountID, targetParentID, strmDirs)
	}
	emby := in.embyAction()
	if scrapeEnabled(in) {
		s.progress.setPhase(progressID, GuangyaSharePhaseScrape)
		result.ScrapeStarted, result.ScrapeError = s.scrapeAfterTransfer(ctx, in.ScrapeTaskID, strmDirs)
	}
	if result.ScrapeStarted {
		asyncPending = true
		if emby.enabled {
			result.EmbyScheduled = true
			s.scheduleEmbyAfterScrape(progressID, in.ScrapeTaskID, emby)
		} else {
			s.scheduleScrapeFinish(progressID, in.ScrapeTaskID)
		}
	} else if emby.enabled {
		s.progress.setPhase(progressID, GuangyaSharePhaseEmby)
		result.EmbyRefreshed, result.EmbyError = s.refreshEmbyNow(ctx, emby)
	}

	if linkID > 0 {
		storedTitle := sanitizeFolderName(in.Title)
		if len(targetPath) > 0 {
			storedTitle = targetPath[0]
		}
		s.saveDestination(ctx, linkID, &domain.GuangyaShareDestination{
			AccountID:     in.AccountID,
			ParentID:      strings.TrimSpace(in.ParentID),
			BasePath:      normalizeDestinationBasePath(in.BasePath),
			BaseDirs:      append([]string(nil), in.BaseDirs...),
			Title:         storedTitle,
			Season:        clampShareSeason(in.Season),
			IsSeries:      in.IsSeries,
			AutoStrm:      in.AutoStrm,
			AutoScrape:    in.AutoScrape,
			ScrapeTaskID:  in.ScrapeTaskID,
			RefreshEmby:   in.RefreshEmby,
			EmbyConfigID:  strings.TrimSpace(in.EmbyConfigID),
			EmbyMode:      normalizeEmbyMode(in.EmbyMode),
			EmbyLibraryID: strings.TrimSpace(in.EmbyLibraryID),
		})
	}
	s.logger().Info("磁力入库完成", "account_id", in.AccountID, "count", len(fileIDs), "target", targetParentID, "renamed", result.Renamed)
	return result, nil
}

// listMagnetDir 列出磁力离线目录内的子目录（浏览用）。
// 目录 ID 分账号：优先用解析时记录的账号；没有记录时才遍历启用账号兜底。
// 注意光鸭列「不存在的目录」也返回空列表（不报错），不能靠报错区分账号，必须先查记录。
func (s *Service) listMagnetDir(ctx context.Context, shareID, parentID string) ([]ShareItem, error) {
	if strings.TrimSpace(parentID) == "" {
		// 根级浏览（不应发生在磁力收藏：ParseMagnet 已返回离线目录内容）；按空目录处理。
		return []ShareItem{}, nil
	}
	accounts, err := s.accounts.List(ctx)
	if err != nil {
		return nil, err
	}
	active := make([]*domain.Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil && account.DriverType == "guangya" && account.IsActive {
			active = append(active, account)
		}
	}
	if len(active) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "没有可用的光鸭云盘账号")
	}
	// 记录的账号排最前，其余账号兜底。
	ordered := make([]*domain.Account, 0, len(active))
	if remembered := s.loadMagnetAccount(ctx, shareID); remembered > 0 {
		for _, account := range active {
			if account.ID == remembered {
				ordered = append(ordered, account)
				break
			}
		}
	}
	for _, account := range active {
		if len(ordered) == 0 || ordered[0].ID != account.ID {
			ordered = append(ordered, account)
		}
	}
	var lastErr error
	for _, account := range ordered {
		if account == nil || account.DriverType != "guangya" || !account.IsActive {
			continue
		}
		var items []ShareItem
		err := s.exec.Run(ctx, account.ID, func(drv driver.Driver) error {
			lister, ok := drv.(driver.Lister)
			if !ok {
				return domain.Errorf(domain.CodeNotImplement, "当前账号驱动不支持列目录")
			}
			files, listErr := lister.ListFiles(ctx, parentID)
			if listErr != nil {
				return listErr
			}
			items = make([]ShareItem, 0, len(files))
			for _, file := range files {
				items = append(items, ShareItem{
					ID:      file.ID,
					Name:    file.Name,
					IsDir:   file.IsDir,
					Size:    file.Size,
					ModTime: file.ModTime,
					Kind:    classifyKind(file.Name, 0, file.IsDir),
				})
			}
			return nil
		})
		if err == nil {
			return items, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = domain.Errorf(domain.CodeValidation, "没有可用的光鸭云盘账号")
	}
	return nil, lastErr
}
