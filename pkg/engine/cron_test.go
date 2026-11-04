package engine

import (
	"testing"
	"time"
)

// MatchCrontab 表测（T3.1）：全星 / 步进 / 范围 / 列表 / 星期日双写 / 越界报错。
// 样本时刻 = 2026-11-05（周四=4）03:07。
func TestMatchCrontab(t *testing.T) {
	tm := time.Date(2026, 11, 5, 3, 7, 30, 0, time.Local)

	tests := []struct {
		expr string
		want bool
	}{
		{"* * * * *", true},
		{"7 3 5 11 4", true},  // 全域精确命中
		{"7 3 5 11 *", true},  // 忽略星期
		{"7 3 * * *", true},   // 日月周全任意
		{"0 3 * * *", false},  // 分不中
		{"*/5 * * * *", false},// 0,5,10...7 不在步点上
		{"*/7 * * * *", true}, // 0,7,...7 命中
		{"*/3 * * * *", false},// 7-0)%3=1 不中
		{"10-20 * * * *", false}, // 范围不含 7
		{"5-20 * * * *", true},   // 范围含 7
		{"6-9/2 * * * *", false}, // 范围+步进：6,8,...7 不在步点
		{"1,7,30 * * * *", true}, // 列表命中
		{"1,2,30 * * * *", false},// 列表不中
		{"0 0 * * 0", false},  // 周日 00:00，今天周四
		{"0 0 * * 7", false},  // 7≡周日 双写，周四不中
	}
	for _, tt := range tests {
		got, err := MatchCrontab(tt.expr, tm)
		if err != nil {
			t.Errorf("MatchCrontab(%q) unexpected error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("MatchCrontab(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}

	// 星期日双写命中：2026-11-01 是周日 00:00
	sun := time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)
	for _, expr := range []string{"0 0 * * 0", "0 0 * * 7"} {
		if ok, err := MatchCrontab(expr, sun); err != nil || !ok {
			t.Errorf("sunday should match %q (err=%v)", expr, err)
		}
	}

	// 非法表达式必须报错（坏 cron 当场暴露，不许变哑弹）
	for _, expr := range []string{"60 * * * *", "* * * *", "a * * * *", "* * * * * *", "1-5 * * * * *"} {
		if _, err := MatchCrontab(expr, tm); err == nil {
			t.Errorf("MatchCrontab(%q) should error", expr)
		}
	}
}
