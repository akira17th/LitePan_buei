package api

import (
	"net/http"

	"litepan/internal/buei/guangyashare"
	"litepan/internal/domain"
)

type bueiGuangyaShareItemDTO struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size"`
	ModTime  string `json:"mod_time,omitempty"`
	FileType int    `json:"file_type"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Depth    int    `json:"depth"`
}

type bueiGuangyaShareDestinationDTO struct {
	AccountID int64    `json:"account_id"`
	ParentID  string   `json:"parent_id"`
	BasePath  string   `json:"base_path"`
	Title     string   `json:"title"`
	Season    int      `json:"season"`
	IsSeries  bool     `json:"is_series"`
	BaseDirs  []string `json:"base_dirs,omitempty"`
}

type bueiGuangyaShareDestinationBaseDTO struct {
	AccountID int64    `json:"account_id"`
	ParentID  string   `json:"parent_id"`
	Path      string   `json:"path"`
	Dirs      []string `json:"dirs,omitempty"`
}

type bueiGuangyaShareBrowseCrumbDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type bueiGuangyaShareTransferredDTO struct {
	FileID         string `json:"file_id"`
	OriginalName   string `json:"original_name"`
	NewName        string `json:"new_name"`
	AccountID      int64  `json:"account_id"`
	TargetParentID string `json:"target_parent_id"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updated_at"`
}

type bueiGuangyaShareRenameReplaceDTO struct {
	Enabled     bool   `json:"enabled"`
	Find        string `json:"find"`
	ReplaceWith string `json:"replace_with"`
}

type bueiGuangyaShareParseDTO struct {
	ShareID          string                              `json:"share_id"`
	Title            string                              `json:"title"`
	Owner            string                              `json:"owner"`
	NeedCode         bool                                `json:"need_code"`
	NeedPassword     bool                                `json:"need_password"`
	Items            []bueiGuangyaShareItemDTO           `json:"items"`
	LastDestination  *bueiGuangyaShareDestinationDTO     `json:"last_destination,omitempty"`
	LastBase         *bueiGuangyaShareDestinationBaseDTO `json:"last_base,omitempty"`
	TransferredFiles []bueiGuangyaShareTransferredDTO    `json:"transferred_files,omitempty"`
	LastBrowsePath   []bueiGuangyaShareBrowseCrumbDTO    `json:"last_browse_path,omitempty"`
	RenameReplace    *bueiGuangyaShareRenameReplaceDTO   `json:"rename_replace,omitempty"`
}

func bueiGuangyaShareRenameReplaceToDTO(rule *domain.GuangyaShareRenameReplace) *bueiGuangyaShareRenameReplaceDTO {
	if rule == nil {
		return nil
	}
	return &bueiGuangyaShareRenameReplaceDTO{
		Enabled:     rule.Enabled,
		Find:        rule.Find,
		ReplaceWith: rule.ReplaceWith,
	}
}

func bueiGuangyaShareBrowsePathToDTO(crumbs []domain.GuangyaShareBrowseCrumb) []bueiGuangyaShareBrowseCrumbDTO {
	dtos := make([]bueiGuangyaShareBrowseCrumbDTO, 0, len(crumbs))
	for _, crumb := range crumbs {
		dtos = append(dtos, bueiGuangyaShareBrowseCrumbDTO{ID: crumb.ID, Name: crumb.Name, Path: crumb.Path})
	}
	return dtos
}

func bueiGuangyaShareTransferredToDTO(files []*domain.GuangyaShareTransferredFile) []bueiGuangyaShareTransferredDTO {
	dtos := make([]bueiGuangyaShareTransferredDTO, 0, len(files))
	for _, file := range files {
		if file == nil {
			continue
		}
		dtos = append(dtos, bueiGuangyaShareTransferredDTO{
			FileID:         file.FileID,
			OriginalName:   file.OriginalName,
			NewName:        file.NewName,
			AccountID:      file.AccountID,
			TargetParentID: file.TargetParentID,
			Status:         file.Status,
			UpdatedAt:      FormatAPITime(file.UpdatedAt),
		})
	}
	return dtos
}

func bueiGuangyaShareDestinationToDTO(dest *domain.GuangyaShareDestination) *bueiGuangyaShareDestinationDTO {
	if dest == nil {
		return nil
	}
	return &bueiGuangyaShareDestinationDTO{
		AccountID: dest.AccountID,
		ParentID:  dest.ParentID,
		BasePath:  dest.BasePath,
		Title:     dest.Title,
		Season:    dest.Season,
		IsSeries:  dest.IsSeries,
		BaseDirs:  dest.BaseDirs,
	}
}

func bueiGuangyaShareDestinationBaseToDTO(base *domain.GuangyaShareDestinationBase) *bueiGuangyaShareDestinationBaseDTO {
	if base == nil {
		return nil
	}
	return &bueiGuangyaShareDestinationBaseDTO{
		AccountID: base.AccountID,
		ParentID:  base.ParentID,
		Path:      base.Path,
		Dirs:      base.Dirs,
	}
}

func bueiGuangyaShareItemsToDTO(items []guangyashare.ShareItem) []bueiGuangyaShareItemDTO {
	dtos := make([]bueiGuangyaShareItemDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, bueiGuangyaShareItemDTO{
			ID:       item.ID,
			ParentID: item.ParentID,
			Name:     item.Name,
			IsDir:    item.IsDir,
			Size:     item.Size,
			ModTime:  FormatAPITime(item.ModTime),
			FileType: item.FileType,
			Kind:     item.Kind,
			Path:     item.Path,
			Depth:    item.Depth,
		})
	}
	return dtos
}

func (h *Handler) bueiGuangyaShareParse(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.GuangyaShare != nil) {
		return
	}
	var in struct {
		URL      string `json:"url"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := h.buei.GuangyaShare.Parse(r.Context(), guangyashare.ParseInput{
		URL:      in.URL,
		Password: in.Password,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, bueiGuangyaShareParseDTO{
		ShareID:          res.ShareID,
		Title:            res.Title,
		Owner:            res.Owner,
		NeedCode:         res.NeedCode,
		NeedPassword:     res.NeedPassword,
		Items:            bueiGuangyaShareItemsToDTO(res.Items),
		LastDestination:  bueiGuangyaShareDestinationToDTO(res.LastDestination),
		LastBase:         bueiGuangyaShareDestinationBaseToDTO(res.LastBase),
		TransferredFiles: bueiGuangyaShareTransferredToDTO(res.TransferredFiles),
		LastBrowsePath:   bueiGuangyaShareBrowsePathToDTO(res.LastBrowsePath),
		RenameReplace:    bueiGuangyaShareRenameReplaceToDTO(res.RenameReplace),
	})
}

func (h *Handler) bueiGuangyaShareList(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.GuangyaShare != nil) {
		return
	}
	var in struct {
		URL        string `json:"url"`
		ShareID    string `json:"share_id"`
		Password   string `json:"password"`
		ParentID   string `json:"parent_id"`
		ParentPath string `json:"parent_path"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := h.buei.GuangyaShare.List(r.Context(), guangyashare.ListInput{
		URL:        in.URL,
		ShareID:    in.ShareID,
		Password:   in.Password,
		ParentID:   in.ParentID,
		ParentPath: in.ParentPath,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"items": bueiGuangyaShareItemsToDTO(res.Items),
	})
}

func (h *Handler) bueiGuangyaShareTransfer(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.GuangyaShare != nil) {
		return
	}
	var in struct {
		URL        string   `json:"url"`
		ShareID    string   `json:"share_id"`
		Password   string   `json:"password"`
		FileIDs    []string `json:"file_ids"`
		AccountID  int64    `json:"account_id"`
		ParentID   string   `json:"parent_id"`
		TargetPath []string `json:"target_path"`
		BasePath   string   `json:"base_path"`
		BaseDirs   []string `json:"base_dirs"`
		Title      string   `json:"title"`
		Season     int      `json:"season"`
		IsSeries   bool     `json:"is_series"`
		Files      []struct {
			FileID       string `json:"file_id"`
			OriginalName string `json:"original_name"`
			NewName      string `json:"new_name"`
			IsDir        bool   `json:"is_dir"`
		} `json:"files"`
		BrowsePath []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"browse_path"`
		RenameReplace *struct {
			Enabled     bool   `json:"enabled"`
			Find        string `json:"find"`
			ReplaceWith string `json:"replace_with"`
		} `json:"rename_replace"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	var renameReplace *domain.GuangyaShareRenameReplace
	if in.RenameReplace != nil {
		renameReplace = &domain.GuangyaShareRenameReplace{
			Enabled:     in.RenameReplace.Enabled,
			Find:        in.RenameReplace.Find,
			ReplaceWith: in.RenameReplace.ReplaceWith,
		}
	}
	browsePath := make([]domain.GuangyaShareBrowseCrumb, 0, len(in.BrowsePath))
	for _, crumb := range in.BrowsePath {
		browsePath = append(browsePath, domain.GuangyaShareBrowseCrumb{ID: crumb.ID, Name: crumb.Name, Path: crumb.Path})
	}
	files := make([]guangyashare.TransferFile, 0, len(in.Files))
	for _, file := range in.Files {
		files = append(files, guangyashare.TransferFile{
			FileID:       file.FileID,
			OriginalName: file.OriginalName,
			NewName:      file.NewName,
			IsDir:        file.IsDir,
		})
	}
	res, err := h.buei.GuangyaShare.Transfer(r.Context(), guangyashare.TransferInput{
		URL:           in.URL,
		ShareID:       in.ShareID,
		Password:      in.Password,
		FileIDs:       in.FileIDs,
		AccountID:     in.AccountID,
		ParentID:      in.ParentID,
		TargetPath:    in.TargetPath,
		BasePath:      in.BasePath,
		BaseDirs:      in.BaseDirs,
		Title:         in.Title,
		Season:        in.Season,
		IsSeries:      in.IsSeries,
		Files:         files,
		BrowsePath:    browsePath,
		RenameReplace: renameReplace,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"task_id":          res.TaskID,
		"completed":        res.Completed,
		"renamed":          res.Renamed,
		"rename_skipped":   res.RenameSkipped,
		"rename_scheduled": res.RenameScheduled,
	})
}

type bueiGuangyaShareSavedLinkDTO struct {
	ID         int64  `json:"id"`
	ShareID    string `json:"share_id"`
	URL        string `json:"url"`
	Password   string `json:"password"`
	Title      string `json:"title"`
	Owner      string `json:"owner"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	LastUsedAt string `json:"last_used_at"`
}

func bueiGuangyaShareSavedLinkToDTO(link *domain.GuangyaShareLink) bueiGuangyaShareSavedLinkDTO {
	return bueiGuangyaShareSavedLinkDTO{
		ID:         link.ID,
		ShareID:    link.ShareID,
		URL:        link.URL,
		Password:   link.Password,
		Title:      link.Title,
		Owner:      link.Owner,
		CreatedAt:  FormatAPITime(link.CreatedAt),
		UpdatedAt:  FormatAPITime(link.UpdatedAt),
		LastUsedAt: FormatAPITime(link.LastUsedAt),
	}
}

func (h *Handler) bueiGuangyaShareDiscard(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.GuangyaShare != nil) {
		return
	}
	var in struct {
		ShareID string `json:"share_id"`
		Files   []struct {
			FileID       string `json:"file_id"`
			OriginalName string `json:"original_name"`
		} `json:"files"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	files := make([]guangyashare.TransferFile, 0, len(in.Files))
	for _, file := range in.Files {
		files = append(files, guangyashare.TransferFile{
			FileID:       file.FileID,
			OriginalName: file.OriginalName,
			NewName:      file.OriginalName,
		})
	}
	count, err := h.buei.GuangyaShare.Discard(r.Context(), guangyashare.DiscardInput{
		ShareID: in.ShareID,
		Files:   files,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"discarded": count})
}

func (h *Handler) bueiGuangyaShareSavedList(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.GuangyaShare != nil) {
		return
	}
	links, err := h.buei.GuangyaShare.SavedLinks(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	dtos := make([]bueiGuangyaShareSavedLinkDTO, 0, len(links))
	for _, link := range links {
		dtos = append(dtos, bueiGuangyaShareSavedLinkToDTO(link))
	}
	writeOK(w, map[string]any{"items": dtos})
}

func (h *Handler) bueiGuangyaShareSavedDelete(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.buei.GuangyaShare != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.buei.GuangyaShare.DeleteSavedLink(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]bool{"deleted": true})
}
