package domain

// IngestUpdateNone 表示收藏没有设置更新时间。
const IngestUpdateNone = -1

// IngestUpdateSchedule 是「影视入库」收藏的更新时间设置：
// 每「天 / 周几」的某个时刻（24 小时制）左右更新，只用于在收藏打开时提示更新节奏。
//
// Weekday：IngestUpdateNone（-1）表示不设置，0 表示每天，1–7 表示周一–周日。
type IngestUpdateSchedule struct {
	Weekday int
	Hour    int
	Minute  int
}

// IsSet 判断是否设置了更新时间（未设置或数值越界都按未设置处理）。
func (s IngestUpdateSchedule) IsSet() bool {
	return s.Weekday >= 0 && s.Weekday <= 7 &&
		s.Hour >= 0 && s.Hour <= 23 &&
		s.Minute >= 0 && s.Minute <= 59
}

// Normalize 校验并归一化更新时间设置；未设置时返回 IngestUpdateNone / 0 / 0。
func (s IngestUpdateSchedule) Normalize() (IngestUpdateSchedule, error) {
	if s.Weekday == IngestUpdateNone {
		return IngestUpdateSchedule{Weekday: IngestUpdateNone}, nil
	}
	if s.Weekday < 0 || s.Weekday > 7 {
		return IngestUpdateSchedule{}, Errorf(CodeValidation, "更新时间只能选择每天或周一至周日")
	}
	if s.Hour < 0 || s.Hour > 23 {
		return IngestUpdateSchedule{}, Errorf(CodeValidation, "更新时间的小时应在 0–23 之间")
	}
	if s.Minute < 0 || s.Minute > 59 {
		return IngestUpdateSchedule{}, Errorf(CodeValidation, "更新时间的分钟应在 0–59 之间")
	}
	return IngestUpdateSchedule{Weekday: s.Weekday, Hour: s.Hour, Minute: s.Minute}, nil
}
