package patchbin

import (
	"testing"
)

const sampleJSDiff = `diff --git a/foo.js b/foo.js
index 1111111..2222222 100644
--- a/foo.js
+++ b/foo.js
@@ -1,5 +1,9 @@
 module.exports = {};

-function add(a) {
-       return a
+function add(a, b) {
+       return a + b
+}
+
+function sub(a, b) {
+       return a - b
 }
`

func TestSemanticChangesJavaScript(t *testing.T) {
	files, _, err := ParsePatch(sampleJSDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	changes := AnalyzeSemanticChanges(files[0])
	if len(changes) == 0 {
		t.Fatalf("expected semantic changes, got none")
	}

	var sawAdd, sawSub bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s oldSig=%q newSig=%q hunk=%d",
			c.Kind, c.EntityKind, c.Name, c.OldSig, c.NewSig, c.HunkIndex)
		if c.Name == "add" && c.Kind == SemanticSignatureChanged {
			sawAdd = true
		}
		if c.Name == "sub" && c.Kind == SemanticAdded {
			sawSub = true
		}
	}

	if !sawAdd {
		t.Errorf("expected add to be reported as signature_changed")
	}
	if !sawSub {
		t.Errorf("expected sub to be reported as added")
	}
}

const sampleTSDiff = `diff --git a/foo.ts b/foo.ts
index 1111111..2222222 100644
--- a/foo.ts
+++ b/foo.ts
@@ -1,5 +1,9 @@
 export {};

-function add(a: number): number {
-       return a
+function add(a: number, b: number): number {
+       return a + b
+}
+
+function sub(a: number, b: number): number {
+       return a - b
 }
`

func TestSemanticChangesTypeScript(t *testing.T) {
	files, _, err := ParsePatch(sampleTSDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	changes := AnalyzeSemanticChanges(files[0])
	if len(changes) == 0 {
		t.Fatalf("expected semantic changes, got none")
	}

	var sawAdd, sawSub bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s oldSig=%q newSig=%q hunk=%d",
			c.Kind, c.EntityKind, c.Name, c.OldSig, c.NewSig, c.HunkIndex)
		if c.Name == "add" && c.Kind == SemanticSignatureChanged {
			sawAdd = true
		}
		if c.Name == "sub" && c.Kind == SemanticAdded {
			sawSub = true
		}
	}

	if !sawAdd {
		t.Errorf("expected add to be reported as signature_changed")
	}
	if !sawSub {
		t.Errorf("expected sub to be reported as added")
	}
}

const samplePyDiff = `diff --git a/foo.py b/foo.py
index 1111111..2222222 100644
--- a/foo.py
+++ b/foo.py
@@ -1,4 +1,7 @@
 import os

-def add(a):
-       return a
+def add(a, b):
+       return a + b
+
+def sub(a, b):
+       return a - b
`

func TestSemanticChangesPython(t *testing.T) {
	files, _, err := ParsePatch(samplePyDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	changes := AnalyzeSemanticChanges(files[0])
	if len(changes) == 0 {
		t.Fatalf("expected semantic changes, got none")
	}

	var sawAdd, sawSub bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s oldSig=%q newSig=%q hunk=%d",
			c.Kind, c.EntityKind, c.Name, c.OldSig, c.NewSig, c.HunkIndex)
		if c.Name == "add" && c.Kind == SemanticSignatureChanged {
			sawAdd = true
		}
		if c.Name == "sub" && c.Kind == SemanticAdded {
			sawSub = true
		}
	}

	if !sawAdd {
		t.Errorf("expected add to be reported as signature_changed")
	}
	if !sawSub {
		t.Errorf("expected sub to be reported as added")
	}
}

const sampleRustDiff = `diff --git a/foo.rs b/foo.rs
index 1111111..2222222 100644
--- a/foo.rs
+++ b/foo.rs
@@ -1,5 +1,9 @@
 mod foo;

-fn add(a: i32) -> i32 {
-       return a
+fn add(a: i32, b: i32) -> i32 {
+       return a + b
+}
+
+fn sub(a: i32, b: i32) -> i32 {
+       return a - b
 }
`

func TestSemanticChangesRust(t *testing.T) {
	files, _, err := ParsePatch(sampleRustDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	changes := AnalyzeSemanticChanges(files[0])
	if len(changes) == 0 {
		t.Fatalf("expected semantic changes, got none")
	}

	var sawAdd, sawSub bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s oldSig=%q newSig=%q hunk=%d",
			c.Kind, c.EntityKind, c.Name, c.OldSig, c.NewSig, c.HunkIndex)
		if c.Name == "add" && c.Kind == SemanticSignatureChanged {
			sawAdd = true
		}
		if c.Name == "sub" && c.Kind == SemanticAdded {
			sawSub = true
		}
	}

	if !sawAdd {
		t.Errorf("expected add to be reported as signature_changed")
	}
	if !sawSub {
		t.Errorf("expected sub to be reported as added")
	}
}
