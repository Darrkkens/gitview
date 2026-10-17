package gitdiff

import "testing"

func TestParse(t *testing.T) {
	raw := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -10,3 +10,4 @@ func F() {\n a\n-b\n+B\n+c\n d\n\\ No newline at end of file\n" +
		"diff --git a/y b/y\nBinary files a/y and b/y differ\n"
	hunks, binary := Parse(raw)
	if !binary || len(hunks) != 1 {
		t.Fatalf("binary=%v hunks=%d", binary, len(hunks))
	}
	h := hunks[0]
	if h.Section != "func F() {" || h.OldStart != 10 || h.OldLines != 3 || h.NewStart != 10 || h.NewLines != 4 {
		t.Fatalf("header %+v", h)
	}
	want := []Line{
		{Context, 10, 10, "a"}, {Removed, 11, 0, "b"}, {Added, 0, 11, "B"}, {Added, 0, 12, "c"}, {Context, 12, 13, "d"},
	}
	if len(h.Lines) != len(want) {
		t.Fatalf("lines %+v", h.Lines)
	}
	for i, w := range want {
		if h.Lines[i] != w {
			t.Errorf("line %d = %+v, want %+v", i, h.Lines[i], w)
		}
	}
	if a, r := Count(hunks); a != 2 || r != 1 {
		t.Errorf("count = +%d -%d", a, r)
	}
}
