package step

import "pipeline/pkg/models"

// shell 步骤：任意脚本直通（spec.Script 原样作为 sh 片段）。

type shellCtl struct{ spec *models.StepSpec }

func (c *shellCtl) PreRun() (string, error)         { return c.spec.Script, nil }
func (c *shellCtl) AfterRun(*models.StepTask) error { return nil }
