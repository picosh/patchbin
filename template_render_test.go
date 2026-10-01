package patchbin

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestPatchFileTemplateRendersLineDiffOnly(t *testing.T) {
	files, _, err := ParsePatch(sampleDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}

	semanticChanges := AnalyzeSemanticChanges(files[0])
	for i := range semanticChanges {
		semanticChanges[i].HunkAnchor = hunkAnchor(1, "foo.go", semanticChanges[i].HunkIndex)
	}

	hunks := make([]PatchHunk, 0, len(files[0].TextFragments))
	for i, frag := range files[0].TextFragments {
		hunks = append(hunks, PatchHunk{
			Anchor:   hunkAnchor(1, "foo.go", i),
			DiffText: template.HTML(frag.String()),
		})
	}

	pf := &PatchFile{
		File:            files[0],
		DisplayName:     "foo.go",
		FileAnchor:      "patch-1-foo.go",
		Adds:            5,
		Dels:            2,
		Hunks:           hunks,
		SemanticChanges: semanticChanges,
	}

	tmpl := getTemplate("pr.html")
	if tmpl == nil {
		t.Fatalf("getTemplate returned nil")
	}

	patchFileTmpl := tmpl.Lookup("patch-file")
	if patchFileTmpl == nil {
		t.Fatalf("patch-file template not found")
	}

	var buf bytes.Buffer
	if err := patchFileTmpl.Execute(&buf, pf); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := buf.String()
	t.Logf("rendered output:\n%s", out)

	// Per-file details should be pure line-diff now -- the semantic
	// breakdown lives only in the patch-level summary, so repeating it
	// here would be redundant.
	if bytes.Contains(buf.Bytes(), []byte("semantic-diff")) {
		t.Errorf("expected no semantic-diff content inside patch-file, semantic breakdown belongs in the summary only")
	}
	if !bytes.Contains(buf.Bytes(), []byte(`<details class="details-min" open id="patch-1-foo.go">`)) {
		t.Errorf("expected patch-file details element to be open by default")
	}
	if !bytes.Contains(buf.Bytes(), []byte("patch-1-foo.go-hunk-0")) {
		t.Errorf("expected hunk anchor in rendered line diff")
	}
}

func TestPRTimelineRendersComment(t *testing.T) {
	tmpl := getTemplate("pr.html")
	if tmpl == nil {
		t.Fatalf("getTemplate returned nil")
	}

	data := PrDetailData{
		Page:     "pr",
		RepoName: "test-repo",
		Branch:   "main",
		Logs: []EventLogData{
			{
				EventLog: &EventLog{
					Event: "pr_commented",
					Data:  EventData{Comment: "Looks great to me!"},
				},
				UserData: UserData{
					Name: "alice",
				},
				Date: "2026-10-01",
			},
		},
		Pr: PrData{
			ID:    1,
			Title: "Test PR",
		},
		MetaData: MetaData{
			URL: "example.com",
		},
		Patchset: &Patchset{},
		Patch: &PatchData{
			Patch: &Patch{},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "commented") {
		t.Errorf("expected 'commented' in rendered timeline, got:\n%s", out)
	}
	if !strings.Contains(out, "Looks great to me!") {
		t.Errorf("expected comment text in rendered timeline, got:\n%s", out)
	}
	if !strings.Contains(out, "timeline-comment") {
		t.Errorf("expected 'timeline-comment' class in rendered timeline, got:\n%s", out)
	}
}
