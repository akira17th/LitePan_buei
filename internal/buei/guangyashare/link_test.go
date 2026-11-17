package guangyashare

import "testing"

func TestParseShareLink(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantID   string
		wantPass string
		wantErr  bool
	}{
		{
			name:   "plain url",
			input:  "https://www.guangyapan.com/s/1969341399784386635_al18HJA7s7gOUCjJ",
			wantID: "1969341399784386635_al18HJA7s7gOUCjJ",
		},
		{
			name:   "www share path",
			input:  "https://www.guangyapan.com/share/1969341399784386635_al18HJA7s7gOUCjJ?foo=1",
			wantID: "1969341399784386635_al18HJA7s7gOUCjJ",
		},
		{
			name:     "url with code query",
			input:    "https://www.guangyapan.com/s/1969341399784386635_al18HJA7s7gOUCjJ?code=kytt",
			wantID:   "1969341399784386635_al18HJA7s7gOUCjJ",
			wantPass: "kytt",
		},
		{
			name:     "shared text with password",
			input:    "光鸭云盘分享：https://www.guangyapan.com/s/1969341399784386635_al18HJA7s7gOUCjJ （提取码：kytt）",
			wantID:   "1969341399784386635_al18HJA7s7gOUCjJ",
			wantPass: "kytt",
		},
		{
			name:     "text with label and halfwidth colon",
			input:    "链接 https://www.guangyapan.com/s/1969341399784386635_al18HJA7s7gOUCjJ 提取码: a1b2",
			wantID:   "1969341399784386635_al18HJA7s7gOUCjJ",
			wantPass: "a1b2",
		},
		{
			name:   "raw share id",
			input:  "1969341399784386635_al18HJA7s7gOUCjJ",
			wantID: "1969341399784386635_al18HJA7s7gOUCjJ",
		},
		{
			name:   "fragment path",
			input:  "https://www.guangyapan.com/#/list/share/1969341399784386635_al18HJA7s7gOUCjJ",
			wantID: "1969341399784386635_al18HJA7s7gOUCjJ",
		},
		{
			name:    "empty",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "random text",
			input:   "这不是一个分享链接",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseShareLink(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ShareID != tc.wantID {
				t.Fatalf("ShareID = %q, want %q", got.ShareID, tc.wantID)
			}
			if got.Password != tc.wantPass {
				t.Fatalf("Password = %q, want %q", got.Password, tc.wantPass)
			}
		})
	}
}

func TestNormalizeTargetPath(t *testing.T) {
	got, err := normalizeTargetPath([]string{" 剧名 (2026) ", "Season 01"})
	if err != nil {
		t.Fatalf("normalizeTargetPath() error = %v", err)
	}
	if len(got) != 2 || got[0] != "剧名 (2026)" || got[1] != "Season 01" {
		t.Fatalf("normalizeTargetPath() = %#v", got)
	}

	if got, err := normalizeTargetPath([]string{"/", ".."}); err == nil || got != nil {
		t.Fatalf("invalid path should fail, got %#v, %v", got, err)
	}
}

func TestSanitizeFolderName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "  走马上任 (2026)  ", want: "走马上任 (2026)"},
		{in: "a/b\\c:d*e?f\"g<h>i|j", want: "a b c d e f g h i j"},
		{in: "..", want: ""},
		{in: "\x00\x01", want: ""},
	}
	for _, tc := range tests {
		if got := sanitizeFolderName(tc.in); got != tc.want {
			t.Fatalf("sanitizeFolderName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyKind(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		fileType int
		isDir    bool
		want     string
	}{
		{name: "folder", fileName: "电影", isDir: true, want: KindFolder},
		{name: "guangya video type", fileName: "无扩展名", fileType: fileTypeVideo, want: KindVideo},
		{name: "extension video", fileName: "Movie.MKV", want: KindVideo},
		{name: "extension audio", fileName: "song.flac", want: KindAudio},
		{name: "extension image", fileName: "poster.JPG", want: KindImage},
		{name: "extension document", fileName: "book.epub", want: KindDocument},
		{name: "extension archive", fileName: "pack.7z", want: KindArchive},
		{name: "subtitle extension", fileName: "sub.srt", want: KindSubtitle},
		{name: "ass subtitle extension", fileName: "sub.ASS", want: KindSubtitle},
		{name: "guangya subtitle type", fileName: "无扩展名", fileType: fileTypeSubtitle, want: KindSubtitle},
		{name: "subtitle extension overrides document type", fileName: "sub.srt", fileType: fileTypeDocument, want: KindSubtitle},
		{name: "unknown", fileName: "file.xyz", want: KindOther},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyKind(tc.fileName, tc.fileType, tc.isDir); got != tc.want {
				t.Fatalf("classifyKind(%q, %d, %v) = %q, want %q", tc.fileName, tc.fileType, tc.isDir, got, tc.want)
			}
		})
	}
}
