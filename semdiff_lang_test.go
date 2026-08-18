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

const sampleZigDiff = `diff --git a/foo.zig b/foo.zig
index 1111111..2222222 100644
--- a/foo.zig
+++ b/foo.zig
@@ -1,5 +1,14 @@
 const std = @import("std");

-pub fn add(a: i32) i32 {
-       return a;
+pub fn add(a: i32, b: i32) i32 {
+       return a + b;
 }
+
+pub fn sub(a: i32, b: i32) i32 {
+       return a - b;
+}
+
+pub const Point = struct {
+       x: f32,
+       y: f32,
+};
`

func TestSemanticChangesZig(t *testing.T) {
	files, _, err := ParsePatch(sampleZigDiff)
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

	var sawAdd, sawSub, sawPoint bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s oldSig=%q newSig=%q hunk=%d",
			c.Kind, c.EntityKind, c.Name, c.OldSig, c.NewSig, c.HunkIndex)
		if c.Name == "add" && c.Kind == SemanticSignatureChanged {
			sawAdd = true
		}
		if c.Name == "sub" && c.Kind == SemanticAdded {
			sawSub = true
		}
		if c.Name == "Point" && c.Kind == SemanticAdded {
			sawPoint = true
		}
	}

	if !sawAdd {
		t.Errorf("expected add to be reported as signature_changed")
	}
	if !sawSub {
		t.Errorf("expected sub to be reported as added")
	}
	if !sawPoint {
		t.Errorf("expected Point struct to be reported as added")
	}
}

const sampleZigTestsAndTypesDiff = `diff --git a/types.zig b/types.zig
index 1111111..2222222 100644
--- a/types.zig
+++ b/types.zig
@@ -1,2 +1,15 @@
 const std = @import("std");

+pub const Status = enum {
+       ok,
+       err,
+};
+
+pub const Result = union(Status) {
+       ok: u32,
+       err: []const u8,
+};
+
+test "basic test" {
+       try std.testing.expect(true);
+}
`

func TestSemanticChangesZigTestsAndTypes(t *testing.T) {
	files, _, err := ParsePatch(sampleZigTestsAndTypesDiff)
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

	var sawStatus, sawResult, sawTest bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s oldSig=%q newSig=%q hunk=%d",
			c.Kind, c.EntityKind, c.Name, c.OldSig, c.NewSig, c.HunkIndex)
		if c.Name == "Status" && c.Kind == SemanticAdded {
			sawStatus = true
		}
		if c.Name == "Result" && c.Kind == SemanticAdded {
			sawResult = true
		}
		if (c.Name == "\"basic test\"" || c.Name == "basic test") && c.Kind == SemanticAdded {
			sawTest = true
		}
	}

	if !sawStatus {
		t.Errorf("expected Status enum to be reported as added")
	}
	if !sawResult {
		t.Errorf("expected Result union to be reported as added")
	}
	if !sawTest {
		t.Errorf("expected test declaration to be reported as added")
	}
}

const sampleZigEnclosingDiff = `diff --git a/server.zig b/server.zig
index 1111111..2222222 100644
--- a/server.zig
+++ b/server.zig
@@ -40,3 +40,3 @@ pub fn handleRequest(self: *Server, req: Request) !Response {
        var x = 1;
-       var y = 2;
+       var y = 3;
        return res;
`

func TestSemanticChangesZigEnclosingFallback(t *testing.T) {
	files, _, err := ParsePatch(sampleZigEnclosingDiff)
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

	var sawEnclosing bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s hunk=%d", c.Kind, c.EntityKind, c.Name, c.HunkIndex)
		if c.Name == "handleRequest" && c.Kind == SemanticModified {
			sawEnclosing = true
		}
	}

	if !sawEnclosing {
		t.Errorf("expected fallback to report handleRequest as modified")
	}
}
