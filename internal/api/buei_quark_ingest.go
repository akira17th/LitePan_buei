package api

import (
	"net/http"
	"strings"

	"litepan/internal/buei/quarkingest"
	"litepan/internal/domain"
)

type bueiQuarkIngestRenameRuleDTO struct {
	Find        string `json:"find"`
	ReplaceWith string `json:"replace_with"`
}

type bueiQuarkIngestRenameReplaceDTO struct {
	Enabled     bool                           `json:"enabled"`
	Find        string                         `json:"find"`
	ReplaceWith string                         `json:"replace_with"`
	Rules       []bueiQuarkIngestRenameRuleDTO `json:"rules,omitempty"`
}

type bueiQuarkIngestPresetDTO struct {
	AccountID     int64                            `json:"account_id"`
	ParentID      string                           `json:"parent_id"`
	BasePath      string                           `json:"base_path"`
	BaseDirs      []string                         `json:"base_dirs,omitempty"`
	Title         string                           `json:"title"`
	Season        int                              `json:"season"`
	IsSeries      bool                             `json:"is_series"`
	AutoStrm      bool                             `json:"auto_strm"`
	AutoScrape    bool                             `json:"auto_scrape"`
	ScrapeTaskID  int64                            `json:"auto_scrape_task_id"`
	RefreshEmby   bool                             `json:"refresh_emby"`
	EmbyConfigID  string                           `json:"emby_config_id"`
	EmbyMode      string                           `json:"emby_mode"`
	EmbyLibraryID string                           `json:"emby_library_id"`
	RenameReplace *bueiQuarkIngestRenameReplaceDTO `json:"rename_replace,omitempty"`
	UpdatedAt     string                           `json:"updated_at,omitempty"`
	// #region buei:quark ingest flash to guangya
	FlashToGuangya bool     `json:"flash_to_guangya"`
	FlashAccountID int64    `json:"flash_account_id"`
	FlashParentID  string   `json:"flash_parent_id"`
	FlashBasePath  string   `json:"flash_base_path"`
	FlashBaseDirs  []string `json:"flash_base_dirs,omitempty"`
	// #endregion buei:quark ingest flash to guangya
}

func bueiQuarkIngestRenameReplaceToDTO(rule quarkingest.RenameReplace) *bueiQuarkIngestRenameReplaceDTO {
	if !rule.Enabled && len(rule.NormalizeRules()) == 0 {
		return nil
	}
	rules := rule.NormalizeRules()
	dtos := make([]bueiQuarkIngestRenameRuleDTO, 0, len(rules))
	for _, item := range rules {
		dtos = append(dtos, bueiQuarkIngestRenameRuleDTO{Find: item.Find, ReplaceWith: item.ReplaceWith})
	}
	return &bueiQuarkIngestRenameReplaceDTO{
		Enabled:     rule.Enabled,
		Find:        rule.Find,
		ReplaceWith: rule.ReplaceWith,
		Rules:       dtos,
	}
}

func bueiQuarkIngestPresetToDTO(preset *quarkingest.Preset) *bueiQuarkIngestPresetDTO {
	if preset == nil {
		return nil
	}
	dto := &bueiQuarkIngestPresetDTO{
		AccountID:     preset.AccountID,
		ParentID:      preset.ParentID,
		BasePath:      preset.BasePath,
		BaseDirs:      preset.BaseDirs,
		Title:         preset.Title,
		Season:        preset.Season,
		IsSeries:      preset.IsSeries,
		AutoStrm:      preset.AutoStrm,
		AutoScrape:    preset.AutoScrape,
		ScrapeTaskID:  preset.ScrapeTaskID,
		RefreshEmby:   preset.RefreshEmby,
		EmbyConfigID:  preset.EmbyConfigID,
		EmbyMode:      preset.EmbyMode,
		EmbyLibraryID: preset.EmbyLibraryID,
		RenameReplace: bueiQuarkIngestRenameReplaceToDTO(preset.RenameReplace),
		UpdatedAt:     FormatAPITime(preset.UpdatedAt),
		// #region buei:quark ingest flash to guangya
		FlashToGuangya: preset.FlashToGuangya,
		FlashAccountID: preset.FlashAccountID,
		FlashParentID:  preset.FlashParentID,
		FlashBasePath:  preset.FlashBasePath,
		FlashBaseDirs:  preset.FlashBaseDirs,
		// #endregion buei:quark ingest flash to guangya
	}
	return dto
}

func (h *Handler) bueiQuarkIngestPreset(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	accountID, err := parseQueryInt64(r, "account_id")
	if err != nil {
		writeErr(w, err)
		return
	}
	if accountID <= 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "account_id 不能为空"))
		return
	}
	preset, err := h.buei.QuarkIngest.LoadPreset(r.Context(), accountID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiQuarkIngestPresetToDTO(preset))
}

func (h *Handler) bueiQuarkIngestTransfer(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	var in struct {
		AccountID            int64    `json:"account_id"`
		FavoriteID           int64    `json:"favorite_id"`
		SourceParentID       string   `json:"source_parent_id"`
		SourceFolderParentID string   `json:"source_folder_parent_id"`
		SourceFolderName     string   `json:"source_folder_name"`
		FileIDs              []string `json:"file_ids"`
		ParentID             string   `json:"parent_id"`
		TargetPath           []string `json:"target_path"`
		BasePath             string   `json:"base_path"`
		BaseDirs             []string `json:"base_dirs"`
		Title                string   `json:"title"`
		Season               int      `json:"season"`
		IsSeries             bool     `json:"is_series"`
		AutoStrm             bool     `json:"auto_strm"`
		AutoScrape           bool     `json:"auto_scrape"`
		ScrapeTaskID         int64    `json:"auto_scrape_task_id"`
		RefreshEmby          bool     `json:"refresh_emby"`
		EmbyConfigID         string   `json:"emby_config_id"`
		EmbyMode             string   `json:"emby_mode"`
		EmbyLibraryID        string   `json:"emby_library_id"`
		ProgressID           string   `json:"progress_id"`
		// #region buei:quark ingest flash to guangya
		FlashToGuangya bool  `json:"flash_to_guangya"`
		FlashAccountID int64 `json:"flash_account_id"`
		// #endregion buei:quark ingest flash to guangya
		Files []struct {
			FileID       string `json:"file_id"`
			OriginalName string `json:"original_name"`
			NewName      string `json:"new_name"`
			IsDir        bool   `json:"is_dir"`
		} `json:"files"`
		RenameReplace *struct {
			Enabled     bool   `json:"enabled"`
			Find        string `json:"find"`
			ReplaceWith string `json:"replace_with"`
			Rules       []struct {
				Find        string `json:"find"`
				ReplaceWith string `json:"replace_with"`
			} `json:"rules"`
		} `json:"rename_replace"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	files := make([]quarkingest.IngestFile, 0, len(in.Files))
	for _, file := range in.Files {
		files = append(files, quarkingest.IngestFile{
			FileID:       file.FileID,
			OriginalName: file.OriginalName,
			NewName:      file.NewName,
			IsDir:        file.IsDir,
		})
	}
	var renameReplace *quarkingest.RenameReplace
	if in.RenameReplace != nil {
		rules := make([]quarkingest.RenameRule, 0, len(in.RenameReplace.Rules))
		for _, rule := range in.RenameReplace.Rules {
			rules = append(rules, quarkingest.RenameRule{Find: rule.Find, ReplaceWith: rule.ReplaceWith})
		}
		renameReplace = &quarkingest.RenameReplace{
			Enabled:     in.RenameReplace.Enabled,
			Find:        in.RenameReplace.Find,
			ReplaceWith: in.RenameReplace.ReplaceWith,
			Rules:       rules,
		}
	}
	res, err := h.buei.QuarkIngest.Ingest(r.Context(), quarkingest.IngestInput{
		AccountID:            in.AccountID,
		FavoriteID:           in.FavoriteID,
		SourceParentID:       in.SourceParentID,
		SourceFolderParentID: in.SourceFolderParentID,
		SourceFolderName:     in.SourceFolderName,
		FileIDs:              in.FileIDs,
		Files:                files,
		ParentID:             in.ParentID,
		TargetPath:           in.TargetPath,
		BasePath:             in.BasePath,
		BaseDirs:             in.BaseDirs,
		Title:                in.Title,
		Season:               in.Season,
		IsSeries:             in.IsSeries,
		AutoStrm:             in.AutoStrm,
		AutoScrape:           in.AutoScrape,
		ScrapeTaskID:         in.ScrapeTaskID,
		RefreshEmby:          in.RefreshEmby,
		EmbyConfigID:         in.EmbyConfigID,
		EmbyMode:             in.EmbyMode,
		EmbyLibraryID:        in.EmbyLibraryID,
		RenameReplace:        renameReplace,
		ProgressID:           in.ProgressID,
		// #region buei:quark ingest flash to guangya
		FlashToGuangya: in.FlashToGuangya,
		FlashAccountID: in.FlashAccountID,
		// #endregion buei:quark ingest flash to guangya
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"moved":          res.Moved,
		"renamed":        res.Renamed,
		"rename_skipped": res.RenameSkipped,
		"flashed":        res.Flashed,     // buei:quark ingest flash to guangya
		"missed":         res.Missed,      // buei:quark ingest flash to guangya
		"miss_message":   res.MissMessage, // buei:quark ingest flash to guangya
		"strm_generated": res.StrmGenerated,
		"strm_matched":   res.StrmMatched,
		"strm_error":     res.StrmError,
		"scrape_started": res.ScrapeStarted,
		"scrape_error":   res.ScrapeError,
		"emby_refreshed": res.EmbyRefreshed,
		"emby_scheduled": res.EmbyScheduled,
		"emby_error":     res.EmbyError,
	})
}

func (h *Handler) bueiQuarkIngestStaged(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	accountID, err := parseQueryInt64(r, "account_id")
	if err != nil {
		writeErr(w, err)
		return
	}
	favoriteID, err := parseQueryInt64(r, "favorite_id")
	if err != nil {
		writeErr(w, err)
		return
	}
	items, err := h.buei.QuarkIngest.FlashStaged(r.Context(), accountID, favoriteID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"file_id":       item.FileID,
			"file_name":     item.FileName,
			"original_name": item.OriginalName,
			"size":          item.Size,
			"updated_at":    FormatAPITime(item.UpdatedAt),
		})
	}
	writeOK(w, map[string]any{"items": out})
}

func (h *Handler) bueiQuarkIngestProgress(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "缺少进度标识"))
		return
	}
	progress, ok := h.buei.QuarkIngest.TransferProgress(id)
	writeOK(w, map[string]any{
		"found": ok,
		"phase": progress.Phase,
		"error": progress.Error,
		// #region buei:quark ingest flash to guangya
		"file":       progress.File,
		"downloaded": progress.Downloaded,
		"total":      progress.Total,
		"speed":      progress.Speed,
		"files":      progress.Files,
		"file_id":    progress.FileID,
		// #endregion buei:quark ingest flash to guangya
	})
}

// #region buei:quark ingest flash to guangya
func (h *Handler) bueiQuarkIngestCancel(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	var in struct {
		ProgressID string `json:"progress_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	id := strings.TrimSpace(in.ProgressID)
	if id == "" {
		writeErr(w, domain.Errorf(domain.CodeValidation, "缺少进度标识"))
		return
	}
	writeOK(w, map[string]bool{"canceled": h.buei.QuarkIngest.CancelIngest(id)})
}

// #endregion buei:quark ingest flash to guangya

type bueiQuarkIngestFavoriteDTO struct {
	ID             int64  `json:"id"`
	AccountID      int64  `json:"account_id"`
	Link           string `json:"link"`
	FolderID       string `json:"folder_id"`
	ParentFolderID string `json:"parent_folder_id"`
	FolderName     string `json:"folder_name"`
	CreatedAt      string `json:"created_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

type bueiQuarkIngestFavoriteBody struct {
	AccountID      int64  `json:"account_id"`
	Link           string `json:"link"`
	FolderID       string `json:"folder_id"`
	ParentFolderID string `json:"parent_folder_id"`
	FolderName     string `json:"folder_name"`
}

func bueiQuarkIngestFavoriteToDTO(favorite *domain.QuarkIngestFavorite) *bueiQuarkIngestFavoriteDTO {
	if favorite == nil {
		return nil
	}
	return &bueiQuarkIngestFavoriteDTO{
		ID:             favorite.ID,
		AccountID:      favorite.AccountID,
		Link:           favorite.Link,
		FolderID:       favorite.FolderID,
		ParentFolderID: favorite.ParentFolderID,
		FolderName:     favorite.FolderName,
		CreatedAt:      FormatAPITime(favorite.CreatedAt),
		UpdatedAt:      FormatAPITime(favorite.UpdatedAt),
	}
}

func (h *Handler) bueiQuarkIngestFavorites(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	accountID, err := parseQueryInt64(r, "account_id")
	if err != nil {
		writeErr(w, err)
		return
	}
	favorites, err := h.buei.QuarkIngest.ListFavorites(r.Context(), accountID)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]*bueiQuarkIngestFavoriteDTO, 0, len(favorites))
	for _, favorite := range favorites {
		items = append(items, bueiQuarkIngestFavoriteToDTO(favorite))
	}
	writeOK(w, map[string]any{"items": items})
}

func (h *Handler) bueiQuarkIngestFavoriteCreate(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	var body bueiQuarkIngestFavoriteBody
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	favorite, err := h.buei.QuarkIngest.CreateFavorite(r.Context(), quarkingest.FavoriteInput{
		AccountID:      body.AccountID,
		Link:           body.Link,
		FolderID:       body.FolderID,
		ParentFolderID: body.ParentFolderID,
		FolderName:     body.FolderName,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiQuarkIngestFavoriteToDTO(favorite))
}

func (h *Handler) bueiQuarkIngestFavoriteUpdate(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body bueiQuarkIngestFavoriteBody
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	favorite, err := h.buei.QuarkIngest.UpdateFavorite(r.Context(), id, quarkingest.FavoriteInput{
		AccountID:      body.AccountID,
		Link:           body.Link,
		FolderID:       body.FolderID,
		ParentFolderID: body.ParentFolderID,
		FolderName:     body.FolderName,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiQuarkIngestFavoriteToDTO(favorite))
}

func (h *Handler) bueiQuarkIngestFavoriteDelete(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	accountID, err := parseQueryInt64(r, "account_id")
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.buei.QuarkIngest.DeleteFavorite(r.Context(), id, accountID); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]bool{"deleted": true})
}

func (h *Handler) bueiQuarkIngestFlatten(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.QuarkIngest != nil) {
		return
	}
	var in struct {
		AccountID  int64 `json:"account_id"`
		FavoriteID int64 `json:"favorite_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	result, err := h.buei.QuarkIngest.FlattenFavorite(r.Context(), in.AccountID, in.FavoriteID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"moved":           result.Moved,
		"renamed":         result.Renamed,
		"deleted_folders": result.DeletedFolders,
	})
}
