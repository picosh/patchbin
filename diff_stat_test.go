package patchbin

import (
	"bytes"
	"html"
	"strings"
	"testing"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

func TestComputeDiffStatBasicAndScaling(t *testing.T) {
	files := []*PatchFile{
		{
			File:        &gitdiff.File{OldName: "small.go", NewName: "small.go"},
			DisplayName: "small.go",
			FileAnchor:  "patch-1-small.go",
			Adds:        2,
			Dels:        1,
		},
		{
			File:        &gitdiff.File{OldName: "large.go", NewName: "large.go"},
			DisplayName: "large.go",
			FileAnchor:  "patch-1-large.go",
			Adds:        50,
			Dels:        10,
		},
		{
			File:        &gitdiff.File{OldName: "old.go", NewName: "new.go", IsRename: true},
			DisplayName: "new.go",
			FileAnchor:  "patch-1-new.go",
			Adds:        0,
			Dels:        0,
		},
		{
			File:        &gitdiff.File{OldName: "img.png", NewName: "img.png", IsBinary: true},
			DisplayName: "img.png",
			FileAnchor:  "patch-1-img.png",
		},
	}

	stat := ComputeDiffStat(files)
	if !stat.HasFiles() {
		t.Fatalf("expected HasFiles to be true")
	}
	if stat.FilesChanged != 4 {
		t.Fatalf("FilesChanged = %d, want 4", stat.FilesChanged)
	}
	if stat.TotalAdds != 52 || stat.TotalDels != 11 {
		t.Fatalf("totals = +%d/-%d, want +52/-11", stat.TotalAdds, stat.TotalDels)
	}
	if stat.SummaryText != "4 files changed, 52 insertions(+), 11 deletions(-)" {
		t.Fatalf("SummaryText = %q", stat.SummaryText)
	}

	// small.go has non-zero adds & dels so it must keep at least 1 '+' and 1 '-'
	if !strings.Contains(stat.Files[0].PlusBar, "+") || !strings.Contains(stat.Files[0].MinusBar, "-") {
		t.Errorf("expected small.go to have both + and -, got %q %q", stat.Files[0].PlusBar, stat.Files[0].MinusBar)
	}

	// large.go bar should be scaled to diffStatMaxBarWidth
	largeBarLen := len(stat.Files[1].PlusBar) + len(stat.Files[1].MinusBar)
	if largeBarLen != diffStatMaxBarWidth {
		t.Errorf("large.go bar len = %d, want %d", largeBarLen, diffStatMaxBarWidth)
	}

	// Rename formatting
	if stat.Files[2].DisplayName != "old.go => new.go" {
		t.Errorf("rename DisplayName = %q, want %q", stat.Files[2].DisplayName, "old.go => new.go")
	}

	// Binary file
	if !stat.Files[3].IsBinary {
		t.Errorf("expected img.png to be marked binary")
	}
}

func TestComputeDiffStatSingularSummary(t *testing.T) {
	files := []*PatchFile{
		{
			File:        &gitdiff.File{OldName: "one.go", NewName: "one.go"},
			DisplayName: "one.go",
			FileAnchor:  "patch-1-one.go",
			Adds:        1,
			Dels:        0,
		},
	}

	stat := ComputeDiffStat(files)
	if stat.SummaryText != "1 file changed, 1 insertion(+)" {
		t.Fatalf("SummaryText = %q, want %q", stat.SummaryText, "1 file changed, 1 insertion(+)")
	}
	if stat.Files[0].PlusBar != "+" || stat.Files[0].MinusBar != "" {
		t.Errorf("bars = %q / %q, want + / ''", stat.Files[0].PlusBar, stat.Files[0].MinusBar)
	}
}

func TestPatchDetailRendersDiffStatAboveSemanticSummary(t *testing.T) {
	files, _, err := ParsePatch(sampleDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}

	semanticChanges := AnalyzeSemanticChanges(files[0])
	for i := range semanticChanges {
		semanticChanges[i].HunkAnchor = hunkAnchor(1, "foo.go", semanticChanges[i].HunkIndex)
	}
	summary := SummarizeSemanticChanges(SemanticSummary{}, "foo.go", true, semanticChanges)

	pf := &PatchFile{
		File:            files[0],
		DisplayName:     "foo.go",
		FileAnchor:      "patch-1-foo.go",
		Adds:            5,
		Dels:            2,
		SemanticChanges: semanticChanges,
	}

	data := PrDetailData{
		Patch: &PatchData{
			Patch:           &Patch{ID: 1, Title: "test patch"},
			Url:             "patch-1",
			PatchFiles:      []*PatchFile{pf},
			DiffStat:        ComputeDiffStat([]*PatchFile{pf}),
			SemanticSummary: summary,
		},
	}

	tmpl := getTemplate("pr.html")
	detailTmpl := tmpl.Lookup("patch-detail")
	if detailTmpl == nil {
		t.Fatalf("patch-detail template not found")
	}

	var buf bytes.Buffer
	if err := detailTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := buf.String()
	statIdx := strings.Index(out, "Diff stat")
	semIdx := strings.Index(out, "Semantic diff summary")
	if statIdx == -1 {
		t.Fatalf("expected 'Diff stat' in rendered patch-detail:\n%s", out)
	}
	if semIdx == -1 {
		t.Fatalf("expected 'Semantic diff summary' in rendered patch-detail:\n%s", out)
	}
	if statIdx >= semIdx {
		t.Errorf("expected 'Diff stat' (idx %d) to appear above 'Semantic diff summary' (idx %d)", statIdx, semIdx)
	}
	unescaped := html.UnescapeString(out)
	for _, want := range []string{
		`href="#patch-1-foo.go"`,
		`data-open-details="1"`,
		"+++++",
		"--",
		"1 file changed, 5 insertions(+), 2 deletions(-)",
	} {
		if !strings.Contains(unescaped, want) {
			t.Errorf("expected rendered output to contain %q", want)
		}
	}
}
