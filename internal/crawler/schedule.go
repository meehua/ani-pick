package crawler

import (
	"time"
)

// 自动任务调度的到期判定（纯函数，便于单测）。
// 两种口径的锚点：
//   - interval：距该任务上一次「运行结束」满 EveryMin 分钟即可（保持与旧版一致的
//     防抖语义——增量看上轮增量结束、过期重爬看任意一轮结束，人工跑过的也算占用）。
//   - daily / every_days：按服务器本地时间的时隙判定——「到了当天的点」且「距上次
//     自动触发的日期已满间隔天数」。锚点是上次自动触发的开始时间（人工运行不算
//     占用，到点就该跑；错过时隙如停机，恢复后补一次）。

// intervalDue：lastFinished 为 nil（从未跑过）视为到期。
func intervalDue(now time.Time, lastFinished *time.Time, everyMin int) bool {
	if lastFinished == nil {
		return true
	}
	return now.Sub(*lastFinished) >= time.Duration(everyMin)*time.Minute
}

// dailyDue：everyDays=1 即每日定时。lastFire 为上次自动触发（任意状态）的开始
// 时间，nil = 从未自动触发过——那也必须等到第一个时隙才跑，不抢跑。
func dailyDue(now time.Time, everyDays, atMinute int, lastFire *time.Time) bool {
	now = now.Local()
	if now.Hour()*60+now.Minute() < atMinute {
		return false // 还没到今天的点
	}
	if lastFire == nil {
		return true
	}
	fire := lastFire.Local()
	fireMid := time.Date(fire.Year(), fire.Month(), fire.Day(), 0, 0, 0, 0, fire.Location())
	todayMid := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := int(todayMid.Sub(fireMid).Hours() / 24)
	return days >= everyDays
}
