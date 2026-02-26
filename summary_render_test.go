package patchbin

import (
	"bytes"
	"testing"
)

func TestSemanticSummaryTemplateRenders(t *testing.T) {
	files, _, err := ParsePatch(sampleDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}

	semanticChanges := AnalyzeSemanticChanges(files[0])
	for i := range semanticChanges {
		semanticChanges[i].HunkAnchor = hunkAnchor(1, "foo.go", semanticChanges[i].HunkIndex)
	}

	summary := SummarizeSemanticChanges(SemanticSummary{}, "foo.go", true, semanticChanges)
	summary = SummarizeSemanticChanges(summary, "go.sum", false, nil)

	pf := &PatchFile{
		File:            files[0],
		DisplayName:     "foo.go",
		FileAnchor:      "patch-1-foo.go",
		Adds:            5,
		Dels:            2,
		SemanticChanges: semanticChanges,
	}

	pd := &PatchData{
		PatchFiles:      []*PatchFile{pf},
		SemanticSummary: summary,
	}

	tmpl := getTemplate("pr.html")
	summaryTmpl := tmpl.Lookup("semantic-summary")
	if summaryTmpl == nil {
		t.Fatalf("semantic-summary template not found")
	}

	var buf bytes.Buffer
	if err := summaryTmpl.Execute(&buf, pd); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := buf.String()
	t.Logf("rendered output:\n%s", out)

	for _, want := range []string{
		"Semantic diff summary",
		"foo.go",
		"1 added",
		"1 signature changed",
		"1 analyzed file",
		"1 file skipped",
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}
