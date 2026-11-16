package guangya

import (
	"strings"
	"testing"

	"litepan/internal/domain"
)

func TestDescribeShareRestoreError(t *testing.T) {
	tests := []struct {
		name    string
		raw     error
		want    string
		wantErr bool
	}{
		{
			name: "own share",
			raw:  domain.Errorf(domain.CodeDriverError, "光鸭 API 错误(204)：不能转存自己的分享"),
			want: "不能转存自己分享的文件",
		},
		{
			name: "space limit",
			raw:  domain.Errorf(domain.CodeDriverError, "光鸭 API 错误(157)：空间不足"),
			want: "网盘空间不足，无法转存",
		},
		{
			name: "access token expired",
			raw:  domain.Errorf(domain.CodeDriverError, "光鸭 API 错误(210)：分享accessToken无效"),
			want: "分享访问令牌已失效，请重新解析分享链接",
		},
		{
			name: "plain error",
			raw:  domain.Wrap(domain.CodeDriverError, nil),
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := describeShareRestoreError(tc.raw)
			if tc.want == "" {
				if got != tc.raw {
					t.Fatalf("expected original error, got %v", got)
				}
				return
			}
			if got == nil || !strings.Contains(got.Error(), tc.want) {
				t.Fatalf("describeShareRestoreError() = %v, want contains %q", got, tc.want)
			}
		})
	}
}
