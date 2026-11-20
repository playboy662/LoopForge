package web

import (
	"os/exec"
	"testing"
)

func TestRunEvalProgress(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	if out, err := exec.Command(node, "run_eval_progress_test.js", appBundlePath(t)).CombinedOutput(); err != nil {
		t.Fatalf("run eval progress: %v\n%s", err, out)
	}
}
