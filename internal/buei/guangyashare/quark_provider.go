package guangyashare

import (
	"context"
	"strings"
)

// quarkProvider 是夸克公开分享接口的适配层。
type quarkProvider struct {
	client *quarkShareClient
}

func newQuarkProvider() *quarkProvider {
	return &quarkProvider{client: newQuarkShareClient()}
}

func (p *quarkProvider) Key() string { return ProviderQuark }

func (p *quarkProvider) DisplayName() string { return providerDisplayName(ProviderQuark) }

// Summary 用空口令试探分享：需要提取码时返回 NeedCode，否则顺带带回访问令牌。
// 夸克的标题在目录接口里返回，这里留空，由首次 List 补齐。
func (p *quarkProvider) Summary(ctx context.Context, shareID, password string) (*shareSummary, error) {
	password = strings.TrimSpace(password)
	token, err := p.client.shareToken(ctx, shareID, password)
	if err != nil {
		if isQuarkPasscodeError(err) {
			if password == "" {
				return &shareSummary{ShareID: shareID, NeedCode: true}, nil
			}
			// 分享可能没有提取码，用户误填的口令会被拒绝；忽略口令再试一次。
			if retry, retryErr := p.client.shareToken(ctx, shareID, ""); retryErr == nil {
				return &shareSummary{ShareID: shareID, Token: retry}, nil
			}
		}
		return nil, err
	}
	return &shareSummary{ShareID: shareID, Token: token}, nil
}

func (p *quarkProvider) AccessToken(ctx context.Context, shareID, password string) (string, error) {
	return p.client.shareToken(ctx, shareID, password)
}

func (p *quarkProvider) List(ctx context.Context, token, shareID, parentID string) (*shareListResult, error) {
	items, title, err := p.client.listShareFiles(ctx, token, shareID, parentID)
	if err != nil {
		return nil, err
	}
	return &shareListResult{Items: items, Title: title}, nil
}
