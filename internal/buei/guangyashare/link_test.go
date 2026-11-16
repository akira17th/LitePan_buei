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
		{name: "subtitle falls back to other", fileName: "sub.srt", want: KindOther},
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
