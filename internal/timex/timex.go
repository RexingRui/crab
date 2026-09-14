// Package timex 统一处理业务时区与时间格式。
//
// 时间在库里一律是 Unix 秒，纯日期字段（如 expect_ship_date）一律是 YYYY-MM-DD 字符串。
// 服务器时区可能是 UTC，因此所有「今天/明天/某天的 0 点」都必须走这里，
// 不能直接用 time.Now() 的本地时区默认值。
package timex

import (
	"fmt"
	"time"
)

// DateLayout 是纯日期字段的格式。
const DateLayout = "2006-01-02"

// loc 是业务时区，进程启动时 Init 一次，全局复用。
var loc = time.FixedZone("CST", 8*3600)

// Init 加载业务时区。系统缺少 tzdata 时退回到固定 +08:00，保证行为可预期。
func Init(name string) error {
	if name == "" {
		name = "Asia/Shanghai"
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return fmt.Errorf("load location %q: %w", name, err)
	}
	loc = l
	return nil
}

// Loc 返回业务时区。
func Loc() *time.Location { return loc }

// Now 当前 Unix 秒。
func Now() int64 { return time.Now().Unix() }

// Format 把 Unix 秒格式化为业务时区的 RFC3339 字符串。
func Format(ts int64) string {
	return time.Unix(ts, 0).In(loc).Format(time.RFC3339)
}

// FormatPtr 把可空时间格式化为 *string，nil 原样返回（JSON 输出 null）。
func FormatPtr(ts *int64) *string {
	if ts == nil {
		return nil
	}
	s := Format(*ts)
	return &s
}

// DateKey 返回业务时区下的 YYYYMMDD，用于订单号日序列。
func DateKey(ts int64) string {
	return time.Unix(ts, 0).In(loc).Format("20060102")
}

// DateStr 返回业务时区下的 YYYY-MM-DD。
func DateStr(ts int64) string {
	return time.Unix(ts, 0).In(loc).Format(DateLayout)
}

// Today 今天的 YYYY-MM-DD。
func Today() string { return DateStr(Now()) }

// AddDays 在日期字符串上加减天数，返回 YYYY-MM-DD。
func AddDays(date string, days int) (string, error) {
	t, err := ParseDate(date)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, 0, days).Format(DateLayout), nil
}

// ParseDate 解析 YYYY-MM-DD 为业务时区当天 0 点。
func ParseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("非法日期 %q，应为 YYYY-MM-DD", s)
	}
	return t, nil
}

// ValidDate 判断是否为合法的 YYYY-MM-DD。
func ValidDate(s string) bool {
	_, err := ParseDate(s)
	return err == nil
}

// DayRange 返回某天在业务时区下的 [起, 止] Unix 秒闭区间。
func DayRange(date string) (int64, int64, error) {
	t, err := ParseDate(date)
	if err != nil {
		return 0, 0, err
	}
	start := t.Unix()
	return start, t.AddDate(0, 0, 1).Unix() - 1, nil
}

// Range 返回 [start 日 0 点, end 日 23:59:59] 的 Unix 秒闭区间。
func Range(startDate, endDate string) (int64, int64, error) {
	s, _, err := DayRange(startDate)
	if err != nil {
		return 0, 0, err
	}
	_, e, err := DayRange(endDate)
	if err != nil {
		return 0, 0, err
	}
	return s, e, nil
}
