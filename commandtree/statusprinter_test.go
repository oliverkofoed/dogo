package commandtree

import (
	"testing"

	"github.com/oliverkofoed/dogo/term"
)

func TestPrintCount(t *testing.T) {
	testCount(t, "hello there", len("hello there"))
	testCount(t, term.Reset+"hello there", len("hello there"))
	testCount(t, term.Reset+"hello "+term.Red+"there", len("hello there"))
}

func testCount(t *testing.T, input string, expectedLen int) {
	count := 2
	term.StartBuffer()
	tprint(input, &count)
	term.FlushBuffer(false)

	if expectedLen+2 != count {
		t.Errorf("Did not get expected length %v (got %v) from input %v", expectedLen, count-2, input)
		t.FailNow()
	}
}
