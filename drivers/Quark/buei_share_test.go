package quark

import (
	"context"
	"strings"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

func TestDescribeShareSaveError(t *testing.T) {
	tests := []struct {
		name string
		raw  error
		want string
	}{
		{
			name: "passcode expired",
			raw:  domain.Errorf(domain.CodeDriverError, "夸克接口错误(41010)：分享提取码错误"),
			want: "提取码已失效",
		},
		{
			name: "share gone",
			raw:  domain.Errorf(domain.CodeDriverError, "夸克接口错误(41004)：分享不存在"),
			want: "分享不存在或已失效",
		},
		{
			name: "space limit",
			raw:  domain.Errorf(domain.CodeDriverError, "夸克接口错误(23001)：空间不足"),
			want: "网盘空间不足",
		},
		{
			name: "cookie expired",
			raw:  domain.Errorf(domain.CodeAuthExpired, "夸克 Cookie 认证失败，请重新获取 Cookie"),
			want: "登录状态已失效",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := describeShareSaveError(tc.raw)
			if got == nil || !strings.Contains(got.Error(), tc.want) {
				t.Fatalf("describeShareSaveError() = %v, want contains %q", got, tc.want)
			}
		})
	}
}

func TestRestoreShareRequiresFileTokens(t *testing.T) {
	d := &Driver{}
	_, _, err := d.RestoreShare(context.Background(), driver.ShareRestoreRequest{
		ShareID:     "0e1b2f7f3cd4",
		AccessToken: "stoken",
		Entries:     []driver.ShareRestoreEntry{{ID: "f1"}},
	})
	if err == nil || !strings.Contains(err.Error(), "令牌") {
		t.Fatalf("expected token validation error, got %v", err)
	}
}
