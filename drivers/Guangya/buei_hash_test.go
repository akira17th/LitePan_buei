// #region buei:guangya-local-flash
package guangya

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 期望值由官方前端算法（upload worker）同一实现生成，用于锁定 gcid/cid 规则。
func TestComputeGuangyaHashes(t *testing.T) {
	cases := []struct {
		size int
		gcid string
		cid  string
	}{
		{1000, "6341A894032CC001CA9887527D7E55F055CEB557", "414475341017EC91703435A6F290324818F983E9"},
		{61439, "162BE26D80B0893F3BFD441112C39E40DD3B8EEC", "E87C5BBF844DBE09F648841472F0E571B1B63EF0"},
		{61440, "4350868C4C99D82AAC09FA64AF95A1C886915429", "A4EA69BD9EC2ED4FDD1C29B56754018C14C79796"},
		{200000, "47993A8356E2559A51C6D22616B1B4C376204C71", "68EC83913C37A165F89BB484AA2CA3C22ECA2742"},
		{2100000, "A357DA3D31D177C34CA17F22AF819A804C31FB4C", "1C28B98B169D66322A410E779528A1EFF5A9D614"},
	}
	dir := t.TempDir()
	for _, tc := range cases {
		path := filepath.Join(dir, "sample.bin")
		data := make([]byte, tc.size)
		for i := range data {
			data[i] = byte((i*31 + 7) & 0xFF)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("写入测试文件失败: %v", err)
		}
		gcid, cid, err := computeGuangyaHashes(context.Background(), path)
		if err != nil {
			t.Fatalf("size=%d 计算失败: %v", tc.size, err)
		}
		if gcid != tc.gcid || cid != tc.cid {
			t.Fatalf("size=%d 指纹不一致：got gcid=%s cid=%s, want gcid=%s cid=%s", tc.size, gcid, cid, tc.gcid, tc.cid)
		}
	}
}

func TestGuangyaHashChunkSize(t *testing.T) {
	cases := map[int64]int64{
		0:          256 << 10,
		0x8000000:  256 << 10,
		0x8000001:  512 << 10,
		0x10000000: 512 << 10,
		0x10000001: 1 << 20,
		0x20000000: 1 << 20,
		0x20000001: 2 << 20,
		547969922:  2 << 20,
	}
	for size, want := range cases {
		if got := guangyaHashChunkSize(size); got != want {
			t.Fatalf("size=%d chunk=%d, want %d", size, got, want)
		}
	}
}

func TestGuangyaHashSampleRanges(t *testing.T) {
	small := guangyaHashSampleRanges(61439)
	if len(small) != 1 || small[0].start != 0 || small[0].end != 61439 {
		t.Fatalf("小文件应取全文件，got %+v", small)
	}
	large := guangyaHashSampleRanges(300000)
	want := []guangyaHashRange{{0, 20480}, {100000, 120480}, {279520, 300000}}
	if len(large) != len(want) {
		t.Fatalf("采样区间数量不一致: %+v", large)
	}
	for i := range want {
		if large[i] != want[i] {
			t.Fatalf("采样区间[%d]=%+v, want %+v", i, large[i], want[i])
		}
	}
}

// #endregion buei:guangya-local-flash
