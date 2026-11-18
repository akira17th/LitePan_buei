package guangyashare

import (
	"context"
	"strings"
	"time"
)

// guangyaProvider 是光鸭公开分享接口的适配层。
type guangyaProvider struct {
	client *shareClient
}

func newGuangyaProvider() *guangyaProvider {
	return &guangyaProvider{client: newShareClient()}
}

func (p *guangyaProvider) Key() string { return ProviderGuangya }

func (p *guangyaProvider) DisplayName() string { return providerDisplayName(ProviderGuangya) }

func (p *guangyaProvider) Summary(ctx context.Context, shareID, _ string) (*shareSummary, error) {
	data, err := p.client.shareSummary(ctx, shareID)
	if err != nil {
		return nil, err
	}
	return &shareSummary{
		ShareID:  strings.TrimSpace(data.ShareID),
		Title:    strings.TrimSpace(data.Title),
		Owner:    strings.TrimSpace(data.NickName),
		NeedCode: data.NeedCode,
	}, nil
}

func (p *guangyaProvider) AccessToken(ctx context.Context, shareID, password string) (string, error) {
	return p.client.shareAccessToken(ctx, shareID, password)
}

func (p *guangyaProvider) List(ctx context.Context, token, _ string, parentID string) (*shareListResult, error) {
	entries, err := p.client.listShareFiles(ctx, token, parentID)
	if err != nil {
		return nil, err
	}
	items := make([]shareEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, shareEntry{
			ID:       entry.FileID,
			Name:     strings.TrimSpace(entry.FileName),
			Size:     entry.FileSize,
			IsDir:    entry.ResType == 2,
			ModTime:  guangyaEntryTime(entry),
			FileType: entry.FileType,
		})
	}
	return &shareListResult{Items: items}, nil
}

func guangyaEntryTime(entry guangyaEntry) time.Time {
	if entry.UTime > 0 {
		return time.Unix(entry.UTime, 0)
	}
	if entry.CTime > 0 {
		return time.Unix(entry.CTime, 0)
	}
	return time.Time{}
}
