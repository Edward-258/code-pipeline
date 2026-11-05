// Package cron —— 定时触发调度器：整分钟对齐 ticker 扫 enabled 配置，命中即经
// FireFunc 回调触发（依赖注入，不 import engine）。
package cron

import (
	"log"
	"time"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 定时触发调度器（T3.1，对照 Zadig cron 子服务的极简进程内版）。
// Zadig 用独立 cron 微服务 + asbrstr 先进表达式库；我们单进程 + 1min 对齐 ticker +
// 5 段匹配器（匹配器本体在 models.CronMatch，纯函数归领域模型），零新依赖。
//
// 依赖方向：本包只被 main 装配（import 环风险的根源已拆除）——触发动作经
// FireFunc 回调注入（main 传 engine.TriggerWorkflow），本包不 import engine。
// 表达式写入校验在 api（同走 models.CronMatch），也不 import 本包。

// FireFunc —— 触发动作的注入点：命中即触发一次工作流（返回任务档案供日志记录）
type FireFunc func(workflowName, creator string) (*models.WorkflowTask, error)

// Start —— 常驻调度循环：对齐到整分钟扫一次 enabled 的 cron 配置。
// fire 由 main 注入（engine.TriggerWorkflow），见包注释的依赖方向说明。
func Start(fire FireFunc) {
	for {
		// 对齐到下一个整分钟：crontab 语义就是"分钟级"，从半分钟起跳的 ticker 会漂移
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		time.Sleep(next.Sub(now))
		cronTick(time.Now(), fire)
	}
}

// cronTick —— 每分钟一次：扫 enabled 的 cron，表达式命中当前分钟即触发。
// 单条失败不影响其余（调度器是常驻进程，一条坏配置不许拖死全场）。
func cronTick(now time.Time, fire FireFunc) {
	crons, err := repo.ListCrons()
	if err != nil {
		log.Printf("cron: list: %v", err)
		return
	}
	for _, c := range crons {
		if !c.Enabled {
			continue
		}
		ok, err := models.CronMatch(c.Crontab, now)
		if err != nil {
			log.Printf("cron %q: bad crontab %q: %v", c.Name, c.Crontab, err)
			continue
		}
		if !ok {
			continue
		}
		t, err := fire(c.WorkflowName, "cron:"+c.Name)
		if err != nil {
			log.Printf("cron %q: trigger %q: %v", c.Name, c.WorkflowName, err)
			continue
		}
		log.Printf("cron: %q fired -> task %s", c.Name, t.ID)
	}
}
