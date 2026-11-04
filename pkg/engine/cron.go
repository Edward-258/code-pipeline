package engine

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"pipeline/pkg/repo"
)

// 定时触发（T3.1，对照 Zadig cron 子服务的极简进程内版）。
// Zadig 用独立 cron 微服务 + asbrstr 先进表达式库；我们单进程 + 1min 对齐 ticker +
// 手搓 5 段匹配器（支持 * / */n / n / a-b / 逗号列表），零新依赖。
// 触发动作 = TriggerWorkflow 正常入队（不吃并发插队，同 wf 串行由队列保证）。

func StartCronScheduler() {
	for {
		// 对齐到下一个整分钟：crontab 语义就是"分钟级"，从半分钟起跳的 ticker 会漂移
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		time.Sleep(next.Sub(now))
		cronTick(time.Now())
	}
}

// cronTick —— 每分钟一次：扫 enabled 的 cron，表达式命中当前分钟即触发。
// 单条失败不影响其余（调度器是常驻进程，一条坏配置不许拖死全场）。
func cronTick(now time.Time) {
	crons, err := repo.ListCrons()
	if err != nil {
		log.Printf("cron: list: %v", err)
		return
	}
	for _, c := range crons {
		if !c.Enabled {
			continue
		}
		ok, err := MatchCrontab(c.Crontab, now)
		if err != nil {
			log.Printf("cron %q: bad crontab %q: %v", c.Name, c.Crontab, err)
			continue
		}
		if !ok {
			continue
		}
		t, err := TriggerWorkflow(c.WorkflowName, "cron:"+c.Name)
		if err != nil {
			log.Printf("cron %q: trigger %q: %v", c.Name, c.WorkflowName, err)
			continue
		}
		log.Printf("cron: %q fired -> task %s", c.Name, t.ID)
	}
}

// MatchCrontab —— 5 段 crontab（分 时 日 月 周）匹配给定时刻。
// 字段语法：* | */n | n | a-b | 逗号混合（"0,15,30" / "1-5" / "*/2" 可混用）。
// 星期域 0/7 = 周日（crontab 惯例）。
func MatchCrontab(expr string, t time.Time) (bool, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false, fmt.Errorf("want 5 fields, got %d", len(fields))
	}
	values := []int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	bounds := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, f := range fields {
		hit, err := matchField(f, values[i], bounds[i])
		if err != nil {
			return false, fmt.Errorf("field %d (%q): %w", i+1, f, err)
		}
		if !hit {
			return false, nil
		}
	}
	return true, nil
}

// matchField —— 单域匹配：逗号切分逐个试（每段可为 *、*/n、n、a-b）
func matchField(f string, v int, bound [2]int) (bool, error) {
	for _, part := range strings.Split(f, ",") {
		ok, err := matchFieldPart(part, v, bound)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func matchFieldPart(p string, v int, bound [2]int) (bool, error) {
	lo, hi := bound[0], bound[1]

	step := 1
	if i := strings.Index(p, "/"); i >= 0 {
		n, err := strconv.Atoi(p[i+1:])
		if err != nil || n <= 0 {
			return false, fmt.Errorf("bad step %q", p)
		}
		step = n
		p = p[:i]
	}

	switch {
	case p == "*":
		// */n：域内从下界起步隔 n 命中（0,5,10...；7 也算命中——星期日双写惯例）
		return (v-lo)%step == 0 && v <= hi, nil
	case strings.Contains(p, "-"):
		segs := strings.SplitN(p, "-", 2)
		a, err1 := strconv.Atoi(segs[0])
		b, err2 := strconv.Atoi(segs[1])
		if err1 != nil || err2 != nil || a > b || a < lo || b > hi {
			return false, fmt.Errorf("bad range %q", p)
		}
		if v < a || v > b {
			return false, nil
		}
		return (v-a)%step == 0, nil
	default:
		n, err := strconv.Atoi(p)
		if err != nil || n < lo || n > hi {
			return false, fmt.Errorf("bad value %q", p)
		}
		if v == n || (bound[1] == 7 && n == 7 && v == 0) { // 星期域 7≡周日
			return true, nil
		}
		return false, nil
	}
}
