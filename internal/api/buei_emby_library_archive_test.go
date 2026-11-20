package api

import (
	"reflect"
	"testing"
)

func TestBueiArchiveTargetRelativePath(t *testing.T) {
	if got := bueiArchiveTargetRelativePath("/云影视/电视剧", "/归档影视/剧集", []string{"钟表馆事件 (2026)"}); !reflect.DeepEqual(got, []string{"电视剧"}) {
		t.Fatalf("剧集归档目标相对路径=%v", got)
	}
	if got := bueiArchiveTargetRelativePath("/云影视/电影", "/归档影视/电影", []string{"测试电影 (2026)"}); len(got) != 0 {
		t.Fatalf("电影归档不应重复电影目录：%v", got)
	}
	if got := bueiArchiveTargetRelativePath("/云影视/电视剧", "/归档影视/剧集", []string{"分类", "剧集 (2026)"}); !reflect.DeepEqual(got, []string{"电视剧", "分类"}) {
		t.Fatalf("嵌套剧集归档目标相对路径=%v", got)
	}
}
