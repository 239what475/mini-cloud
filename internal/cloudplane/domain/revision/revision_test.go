package revision

import "testing"

// TestLabelForNumber 验证 revision number 的标签格式。
func TestLabelForNumber(t *testing.T) {
	t.Parallel()

	// revision number 需要格式化为固定宽度标签。
	if got := LabelForNumber(1); got != "r000001" {
		t.Fatalf("LabelForNumber(1) = %q, want %q", got, "r000001")
	}

	// 多位数字同样左侧补零到统一宽度。
	if got := LabelForNumber(42); got != "r000042" {
		t.Fatalf("LabelForNumber(42) = %q, want %q", got, "r000042")
	}
}
