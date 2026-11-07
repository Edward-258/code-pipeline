package step

import (
	"testing"

	"pipeline/pkg/models"
)

// 驱动脚本侧：archive 步骤生成 tar+curl 片段；缺 glob 生成期报错。
// （T4.3 后上传走 curl——busybox wget --post-file 截断 POST body，见 AGENTS.md §9.17）
func TestArchiveDriverGeneration(t *testing.T) {
	steps := []*models.StepSpec{{Name: "pack", Type: models.StepArchive, ArtifactGlob: "dist/*.tgz", ArtifactName: "out.tgz"}}
	script, err := GenerateDriverScript(steps, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, want := range []string{
		"tar czf /tmp/mp-artifact.tgz dist/*.tgz",
		"curl -s -f -X POST -H 'Content-Type: application/gzip' --data-binary @/tmp/mp-artifact.tgz",
		`"$MP_ENGINE_URL/api/tasks/$MP_TASK_WF/$MP_TASK_ID/artifact/out.tgz"`,
	} {
		if !contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
	if _, err := GenerateDriverScript([]*models.StepSpec{{Name: "bad", Type: models.StepArchive}}, nil); err == nil {
		t.Error("archive without artifact_glob should fail at generate time")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
