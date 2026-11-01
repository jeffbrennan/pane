package git

import (
	"strings"
	"testing"
)

func TestSplitPatch(t *testing.T) {
	patch := "diff --git a/one b/one\nindex 1..2\n--- a/one\n+++ b/one\n@@ -1 +1 @@\n-diff --git a/x b/x\n+new\n" +
		"diff --git a/a b/c b/a b/c\n+++ b/a b/c\n+spaced\n" +
		"diff --git \"a/q\\\"t\" \"b/q\\\"t\"\n+quoted\n" +
		"diff --cc conflict\n+combined\n" +
		"diff --git a/link b/link\ndeleted file mode 100644\n" +
		"diff --git a/link b/link\nnew file mode 120000\n"
	patches := splitPatch(patch)
	if len(patches) != 3 {
		t.Fatalf("patches: %#v", patches)
	}
	if !strings.HasPrefix(patches["one"], "diff --git a/one b/one\n") || !strings.HasSuffix(patches["one"], "+new\n") {
		t.Fatalf("one: %q", patches["one"])
	}
	if patches["a b/c"] != "diff --git a/a b/c b/a b/c\n+++ b/a b/c\n+spaced\n" {
		t.Fatalf("spaced: %q", patches["a b/c"])
	}
	if !strings.Contains(patches["link"], "deleted file mode") || !strings.Contains(patches["link"], "new file mode") {
		t.Fatalf("typechange sections not joined: %q", patches["link"])
	}
}

func TestSplitPatchSubmoduleSections(t *testing.T) {
	patch := "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-a\n+b\n" +
		"Submodule space name contains modified content\nSubmodule space name abc1234..def5678:\n  > subject\n" +
		"Submodule new module 0000000...abcdef0 (new submodule)\n"
	sections := splitPatch(patch)
	if len(sections) != 3 || !strings.Contains(sections["space name"], "subject") || !strings.Contains(sections["new module"], "new submodule") || !strings.Contains(sections["file"], "+b") {
		t.Fatalf("sections: %#v", sections)
	}

}
