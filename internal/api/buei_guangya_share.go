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

type bueiGuangyaShareParseDTO struct {
	ShareID      string                    `json:"share_id"`
	Title        string                    `json:"title"`
	Owner        string                    `json:"owner"`
	NeedCode     bool                      `json:"need_code"`
	NeedPassword bool                      `json:"need_password"`
	Items        []bueiGuangyaShareItemDTO `json:"items"`
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
		ShareID:      res.ShareID,
		Title:        res.Title,
		Owner:        res.Owner,
		NeedCode:     res.NeedCode,
		NeedPassword: res.NeedPassword,
		Items:        bueiGuangyaShareItemsToDTO(res.Items),
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
		URL       string   `json:"url"`
		ShareID   string   `json:"share_id"`
		Password  string   `json:"password"`
		FileIDs   []string `json:"file_ids"`
		AccountID int64    `json:"account_id"`
		ParentID  string   `json:"parent_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := h.buei.GuangyaShare.Transfer(r.Context(), guangyashare.TransferInput{
		URL:       in.URL,
		ShareID:   in.ShareID,
		Password:  in.Password,
		FileIDs:   in.FileIDs,
		AccountID: in.AccountID,
		ParentID:  in.ParentID,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"task_id":   res.TaskID,
		"completed": res.Completed,
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
