package app

import "testing"

func TestBueiDirsBelowRoot(t *testing.T) {
	root := bueiRemoteSegments("/光鸭/我的资源")
	cases := []struct {
		name string
		dirs []string
		want []string
		ok   bool
	}{
		{"命中任务根", []string{"光鸭", "我的资源", "剧名", "Season 1"}, []string{"剧名", "Season 1"}, true},
		{"任务根就是目标", []string{"光鸭", "我的资源"}, nil, true},
		{"不在任务根下", []string{"别处", "剧名"}, nil, false},
		{"任务根为空则全部保留", []string{"剧名"}, []string{"剧名"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots := root
			if tc.name == "任务根为空则全部保留" {
				roots = nil
			}
			got, ok := bueiDirsBelowRoot(tc.dirs, roots)
			if ok != tc.ok || len(got) != len(tc.want) {
				t.Fatalf("got=%v/%v want=%v/%v", got, ok, tc.want, tc.ok)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got=%v want=%v", got, tc.want)
				}
			}
		})
	}
}

func TestBueiRemoteSegments(t *testing.T) {
	if got := bueiRemoteSegments(" /光鸭//我的资源/ "); len(got) != 2 || got[0] != "光鸭" || got[1] != "我的资源" {
		t.Fatalf("unexpected segments: %#v", got)
	}
	if got := bueiRemoteSegments("   "); got != nil {
		t.Fatalf("空路径应返回 nil，实际 %#v", got)
	}
}

func TestBueiJoinSafeDirs(t *testing.T) {
	cases := []struct {
		name string
		dirs []string
		want string
	}{
		{"拼成单个相对目录", []string{"电视剧", "国产剧", "目标剧 (2026)", "Season 01"}, "电视剧/国产剧/目标剧 (2026)/Season 01"},
		{"跳过空白段", []string{"电视剧", " ", "目标剧"}, "电视剧/目标剧"},
		{"空段返回空", []string{"", "  "}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bueiJoinSafeDirs(tc.dirs); got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}
