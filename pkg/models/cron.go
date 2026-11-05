package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron —— 定时触发配置（T3.1，对照 Zadig cron 子服务的 CronList 砍到骨头）：
// 一条 cron = 一个工作流的触发规则。5 段 crontab 表达式（分 时 日 月 周），
// 进程内 1min ticker 匹配当前分钟即触发（走 TriggerWorkflow 正常入队，不吃并发插队）。
type Cron struct {
	Name         string    `bson:"_id"           json:"name"` // cron 名即主键
	WorkflowName string    `bson:"workflow_name" json:"workflow_name"`
	Crontab      string    `bson:"crontab"       json:"crontab"` // 5 段：分 时 日 月 周
	Enabled      bool      `bson:"enabled"       json:"enabled"`
	CreatedAt    time.Time `bson:"created_at"    json:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"    json:"updated_at"`
}

// CronMatch —— 5 段 crontab（分 时 日 月 周）匹配给定时刻。
// 字段语法：* | */n | n | a-b | 逗号混合（"0,15,30" / "1-5" / "*/2" 可混用）。
// 星期域 0/7 = 周日（crontab 惯例）。表达式非法报错——坏 cron 当场暴露，不许变哑弹。
// 纯函数归领域模型：cron 调度器与 api 写入校验共用这一个判定器。
func CronMatch(expr string, t time.Time) (bool, error) {
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
