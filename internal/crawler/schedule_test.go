package crawler

import (
	"testing"
	"time"
)

// 调度判定的纯函数测试：覆盖间隔、每日、每隔几天与错过时隙补跑/同隙不重复。

func testTime(h, m int) time.Time {
	return time.Date(2026, 11, 22, h, m, 0, 0, time.Local)
}

func TestIntervalDue(t *testing.T) {
	now := testTime(12, 0)
	if !intervalDue(now, nil, 10) {
		t.Fatal("从未跑过应到期")
	}
	finished := testTime(11, 51)
	if intervalDue(now, &finished, 10) {
		t.Fatal("距结束 9 分钟未满不应到期")
	}
	finished = testTime(11, 49)
	if !intervalDue(now, &finished, 10) {
		t.Fatal("距结束满 10 分钟应到期")
	}
}

func TestDailyDue(t *testing.T) {
	now := testTime(12, 0)
	if !dailyDue(now, 1, 3*60, nil) {
		t.Fatal("从未触发且已过 03:00 应到期")
	}
	if dailyDue(now, 1, 13*60, nil) {
		t.Fatal("还没到当天的点不应到期")
	}
	// 今天 03:05 已经自动跑过 → 03:00 的时隙今天不再触发
	firedToday := testTime(3, 5)
	if dailyDue(now, 1, 3*60, &firedToday) {
		t.Fatal("今天的时隙已跑过不应重复触发")
	}
	// 昨天跑过 → 今天到期
	firedYesterday := time.Date(2026, 11, 21, 3, 5, 0, 0, time.Local)
	if !dailyDue(now, 1, 3*60, &firedYesterday) {
		t.Fatal("上次触发是昨天应到期")
	}
	// 每隔 3 天：仅隔 1 天不触发，隔 3 天触发
	if dailyDue(now, 3, 3*60, &firedYesterday) {
		t.Fatal("每隔 3 天但仅隔 1 天不应触发")
	}
	fired3DaysAgo := time.Date(2026, 11, 19, 3, 0, 0, 0, time.Local)
	if !dailyDue(now, 3, 3*60, &fired3DaysAgo) {
		t.Fatal("每隔 3 天已满应触发")
	}
}
