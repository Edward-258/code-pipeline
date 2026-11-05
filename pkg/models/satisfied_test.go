package models

import "testing"

// TestSatisfied —— T3.4 审批策略真值表：any/空名单/all（原 engine 侧 TestApprovalSatisfied 随方法迁入）
func TestSatisfied(t *testing.T) {
	// 空名单 + 默认策略（老图纸）：一条审批即过
	me := &ManualExec{Approvals: []*Approval{{Approver: "alice"}}}
	if !me.Satisfied() {
		t.Error("empty-roster any policy should pass with one approval")
	}
	// 无记录：不过
	if (&ManualExec{}).Satisfied() {
		t.Error("no approvals must not satisfy")
	}
	// any：任一人批即过
	me = &ManualExec{Policy: ApprovalPolicyAny,
		Approvers: []string{"alice", "bob"}, Approvals: []*Approval{{Approver: "bob"}}}
	if !me.Satisfied() {
		t.Error("any policy should pass with one of the approvers")
	}
	// all：缺一人不过，到齐才过
	me = &ManualExec{Policy: ApprovalPolicyAll,
		Approvers: []string{"alice", "bob"}, Approvals: []*Approval{{Approver: "alice"}}}
	if me.Satisfied() {
		t.Error("all policy must wait for every approver")
	}
	me.Approvals = append(me.Approvals, &Approval{Approver: "bob"})
	if !me.Satisfied() {
		t.Error("all policy should pass when roster complete")
	}
}
