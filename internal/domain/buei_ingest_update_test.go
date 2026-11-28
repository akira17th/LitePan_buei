package domain_test

import (
	"testing"

	"litepan/internal/domain"
)

func TestIngestUpdateScheduleNormalize(t *testing.T) {
	cases := []struct {
		name    string
		in      domain.IngestUpdateSchedule
		want    domain.IngestUpdateSchedule
		wantErr bool
	}{
		{name: "不设置", in: domain.IngestUpdateSchedule{Weekday: domain.IngestUpdateNone}, want: domain.IngestUpdateSchedule{Weekday: domain.IngestUpdateNone}},
		{name: "每天", in: domain.IngestUpdateSchedule{Weekday: 0, Hour: 20, Minute: 30}, want: domain.IngestUpdateSchedule{Weekday: 0, Hour: 20, Minute: 30}},
		{name: "周日", in: domain.IngestUpdateSchedule{Weekday: 7, Hour: 0, Minute: 0}, want: domain.IngestUpdateSchedule{Weekday: 7, Hour: 0, Minute: 0}},
		{name: "星期越界", in: domain.IngestUpdateSchedule{Weekday: 8, Hour: 20}, wantErr: true},
		{name: "小时越界", in: domain.IngestUpdateSchedule{Weekday: 1, Hour: 24}, wantErr: true},
		{name: "分钟越界", in: domain.IngestUpdateSchedule{Weekday: 1, Hour: 20, Minute: 60}, wantErr: true},
	}
	for _, tc := range cases {
		got, err := tc.in.Normalize()
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: 期望校验失败，实际 %+v", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: 不应报错：%v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: 期望 %+v，实际 %+v", tc.name, tc.want, got)
		}
	}
}

func TestIngestUpdateScheduleIsSet(t *testing.T) {
	if (domain.IngestUpdateSchedule{Weekday: domain.IngestUpdateNone}).IsSet() {
		t.Fatal("未设置时 IsSet 应为 false")
	}
	if !(domain.IngestUpdateSchedule{Weekday: 0, Hour: 8, Minute: 5}).IsSet() {
		t.Fatal("每天 08:05 应是已设置")
	}
	if (domain.IngestUpdateSchedule{Weekday: 3, Hour: 30}).IsSet() {
		t.Fatal("小时越界时 IsSet 应为 false")
	}
}
