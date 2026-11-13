package cleanup

import "testing"

func TestAnAnswerAgreesOrDiffers(t *testing.T) {
	if got := verdictOf(true); got != VerdictAgrees {
		t.Errorf("true: %q", got)
	}
	if got := verdictOf(false); got != VerdictDiffers {
		t.Errorf("false: %q", got)
	}
}
