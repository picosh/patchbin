package patchbin

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/styles"
)

const sampleDiffRenderGo = `diff --git a/main.go b/main.go
index 1234567..89abcdef 100644
--- a/main.go
+++ b/main.go
@@ -1,5 +1,6 @@
 package main
 
-func OldFunction() int {
+func NewFunction() string {
+	// added comment
 	return 42
 }
`

const sampleDiffRenderZig = `diff --git a/main.zig b/main.zig
index 1234567..89abcdef 100644
--- a/main.zig
+++ b/main.zig
@@ -1,4 +1,5 @@
 const std = @import("std");
 
-pub fn main() void {
+pub fn main() !void {
+    // zig comment
 }
`

func TestFormatDiffHunkGo(t *testing.T) {
	files, _, err := ParsePatch(sampleDiffRenderGo)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) == 0 || len(files[0].TextFragments) == 0 {
		t.Fatalf("expected at least 1 file and fragment")
	}

	theme := styles.Get("monokai")
	anchor := "patch-1-main.go-hunk-0"
	htmlOut, err := FormatDiffHunk(theme, "main.go", files[0].TextFragments[0], anchor)
	if err != nil {
		t.Fatalf("FormatDiffHunk: %v", err)
	}

	// Verify table structure
	if !strings.Contains(htmlOut, "diff-table") {
		t.Errorf("expected diff-table class in output")
	}
	if !strings.Contains(htmlOut, "diff-line-hunk") {
		t.Errorf("expected diff-line-hunk class in output")
	}

	// Verify line number and diff overlays
	if !strings.Contains(htmlOut, "diff-line-add") {
		t.Errorf("expected diff-line-add class in output")
	}
	if !strings.Contains(htmlOut, "diff-line-delete") {
		t.Errorf("expected diff-line-delete class in output")
	}
	if !strings.Contains(htmlOut, "diff-line-context") {
		t.Errorf("expected diff-line-context class in output")
	}

	// Verify gutter markers
	if !strings.Contains(htmlOut, "<td class=\"diff-gutter\">+</td>") {
		t.Errorf("expected '+' in gutter")
	}
	if !strings.Contains(htmlOut, "<td class=\"diff-gutter\">-</td>") {
		t.Errorf("expected '-' in gutter")
	}

	// Verify anchor links in line numbers
	if !strings.Contains(htmlOut, `id="patch-1-main.go-hunk-0-L3"`) {
		t.Errorf("expected deleted line anchor id in output, got:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `href="#patch-1-main.go-hunk-0-L3"`) {
		t.Errorf("expected deleted line anchor href in output, got:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `id="patch-1-main.go-hunk-0-R3"`) {
		t.Errorf("expected added line anchor id in output, got:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `href="#patch-1-main.go-hunk-0-R3"`) {
		t.Errorf("expected added line anchor href in output, got:\n%s", htmlOut)
	}

	// Verify syntax highlighting (Chroma classes for Go keywords / types / comments)
	if !strings.Contains(htmlOut, "class=\"kd\"") && !strings.Contains(htmlOut, "class=\"k\"") {
		t.Errorf("expected keyword chroma class (kd or k) for 'func' or 'package', got:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, "class=\"c1\"") && !strings.Contains(htmlOut, "class=\"c\"") {
		t.Errorf("expected comment chroma class (c1 or c) for '// added comment', got:\n%s", htmlOut)
	}
}

func TestFormatDiffHunkZig(t *testing.T) {
	files, _, err := ParsePatch(sampleDiffRenderZig)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}

	htmlOut, err := FormatDiffHunk(nil, "src/main.zig", files[0].TextFragments[0], "patch-1-main.zig-hunk-0")
	if err != nil {
		t.Fatalf("FormatDiffHunk: %v", err)
	}

	// Verify Zig syntax highlighting tokens
	if !strings.Contains(htmlOut, "class=\"k\"") && !strings.Contains(htmlOut, "class=\"kd\"") {
		t.Errorf("expected keyword class for 'pub' or 'fn' or 'const', got:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, "diff-line-add") {
		t.Errorf("expected diff-line-add in zig diff")
	}
	if !strings.Contains(htmlOut, `href="#patch-1-main.zig-hunk-0-R3"`) {
		t.Errorf("expected anchor link in zig diff, got:\n%s", htmlOut)
	}
}

func TestFormatDiffHunkFallback(t *testing.T) {
	files, _, err := ParsePatch(sampleDiffRenderGo)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}

	// Unknown extension should not error and still render diff table correctly
	htmlOut, err := FormatDiffHunk(nil, "unknown.xyz", files[0].TextFragments[0], "")
	if err != nil {
		t.Fatalf("FormatDiffHunk: %v", err)
	}

	if !strings.Contains(htmlOut, "diff-table") {
		t.Errorf("expected diff-table class in output")
	}
	if !strings.Contains(htmlOut, "diff-line-add") {
		t.Errorf("expected diff-line-add in unknown file diff")
	}
}
