package patchbin

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	ha "github.com/oddg/hungarian-algorithm"
)

var (
	COST_MAX                           = 65536
	RANGE_DIFF_CREATION_FACTOR_DEFAULT = 60
)

// RangeDiffOutput represents a single commit comparison entry in the range diff.
type RangeDiffOutput struct {
	Header *RangeDiffHeader
	Order  int
	Files  []*RangeDiffFile
	Type   string // "rm", "add", "equal", "changed"
}

// RangeDiffFile represents a file-level change between two matched commits.
type RangeDiffFile struct {
	OldName string
	NewName string
	Type    string // "added", "removed", "changed"
}

// RangeDiffHeader is a header combining old and new commit pairs.
type RangeDiffHeader struct {
	OldIdx         int
	OldSha         string
	OldAuthorName  string
	OldAuthorEmail string
	OldTitle       string
	OldBody        string
	NewIdx         int
	NewSha         string
	NewAuthorName  string
	NewAuthorEmail string
	NewTitle       string
	NewBody        string
	Title          string
	ContentEqual   bool
	AuthorChanged  bool
	TitleChanged   bool
	BodyChanged    bool
}

// NewRangeDiffHeader creates a header from two patch ranges.
func NewRangeDiffHeader(a, b *Patch, aIndex, bIndex int) *RangeDiffHeader {
	hdr := &RangeDiffHeader{}
	if a == nil {
		hdr.NewIdx = bIndex
		hdr.NewSha = b.CommitSha
		hdr.NewAuthorName = b.AuthorName
		hdr.NewAuthorEmail = b.AuthorEmail
		hdr.NewTitle = b.Title
		hdr.NewBody = b.Body
		hdr.Title = b.Title
		return hdr
	}
	if b == nil {
		hdr.OldIdx = aIndex
		hdr.OldSha = a.CommitSha
		hdr.OldAuthorName = a.AuthorName
		hdr.OldAuthorEmail = a.AuthorEmail
		hdr.OldTitle = a.Title
		hdr.OldBody = a.Body
		hdr.Title = a.Title
		return hdr
	}

	hdr.OldIdx = aIndex
	hdr.NewIdx = bIndex
	hdr.OldSha = a.CommitSha
	hdr.NewSha = b.CommitSha
	hdr.OldAuthorName = a.AuthorName
	hdr.OldAuthorEmail = a.AuthorEmail
	hdr.OldTitle = a.Title
	hdr.OldBody = a.Body
	hdr.NewAuthorName = b.AuthorName
	hdr.NewAuthorEmail = b.AuthorEmail
	hdr.NewTitle = b.Title
	hdr.NewBody = b.Body

	// Check what changed
	hdr.AuthorChanged = a.AuthorName != b.AuthorName || a.AuthorEmail != b.AuthorEmail
	hdr.TitleChanged = a.Title != b.Title
	hdr.BodyChanged = a.Body != b.Body

	if a.ContentSha == b.ContentSha {
		hdr.Title = a.Title
		hdr.ContentEqual = true
	} else {
		hdr.Title = b.Title
	}

	return hdr
}

func (hdr *RangeDiffHeader) String() string {
	if hdr.OldIdx == 0 {
		return fmt.Sprintf("-:  ------- > %d:  %s %s\n", hdr.NewIdx, truncateSha(hdr.NewSha), hdr.Title)
	}
	if hdr.NewIdx == 0 {
		return fmt.Sprintf("%d:  %s < -:  ------- %s\n", hdr.OldIdx, truncateSha(hdr.OldSha), hdr.Title)
	}
	if hdr.ContentEqual {
		return fmt.Sprintf(
			"%d:  %s = %d:  %s %s\n",
			hdr.OldIdx, truncateSha(hdr.OldSha),
			hdr.NewIdx, truncateSha(hdr.NewSha),
			hdr.Title,
		)
	}
	return fmt.Sprintf(
		"%d:  %s ! %d:  %s %s\n",
		hdr.OldIdx, truncateSha(hdr.OldSha),
		hdr.NewIdx, truncateSha(hdr.NewSha),
		hdr.Title,
	)
}

// RangeDiff compares two patchsets and returns commit-level changes.
func RangeDiff(a []*Patch, b []*Patch) []*RangeDiffOutput {
	aPatches := make([]*patchEntry, len(a))
	for i, p := range a {
		aPatches[i] = &patchEntry{Patch: p, Matching: -1, Size: patchSize(p)}
	}
	bPatches := make([]*patchEntry, len(b))
	for i, p := range b {
		bPatches[i] = &patchEntry{Patch: p, Matching: -1, Size: patchSize(p)}
	}

	findExactMatches(aPatches, bPatches)
	getCorrespondences(aPatches, bPatches, RANGE_DIFF_CREATION_FACTOR_DEFAULT)
	return buildOutput(aPatches, bPatches)
}

// patchEntry wraps a Patch with matching state for the algorithm.
type patchEntry struct {
	*Patch
	Matching int
	Size     int
}

// patchSize returns a rough size metric for a patch (used for matching cost).
func patchSize(p *Patch) int {
	return len(p.RawText)
}

// buildOutput constructs the final range diff output from matched patches.
func buildOutput(a []*patchEntry, b []*patchEntry) []*RangeDiffOutput {
	outputs := []*RangeDiffOutput{}

	// Removed commits (in A but not matched in B)
	for i, patchA := range a {
		if patchA.Matching == -1 {
			hdr := NewRangeDiffHeader(patchA.Patch, nil, i+1, -1)
			files := filesRemoved(patchA.Patch)
			outputs = append(outputs, &RangeDiffOutput{
				Header: hdr,
				Type:   "rm",
				Order:  i + 1,
				Files:  files,
			})
		}
	}

	// Added or changed commits (from B side)
	for j, entryB := range b {
		if entryB.Matching == -1 {
			// Added commit (in B but not matched in A)
			hdr := NewRangeDiffHeader(nil, entryB.Patch, -1, j+1)
			files := filesAdded(entryB.Patch)
			outputs = append(outputs, &RangeDiffOutput{
				Header: hdr,
				Type:   "add",
				Order:  j + 1,
				Files:  files,
			})
			continue
		}

		entryA := a[entryB.Matching]
		if entryB.ContentSha == entryA.ContentSha {
			// Equal commits
			hdr := NewRangeDiffHeader(entryA.Patch, entryB.Patch, entryB.Matching+1, entryA.Matching+1)
			outputs = append(outputs, &RangeDiffOutput{
				Header: hdr,
				Type:   "equal",
				Order:  entryA.Matching + 1,
			})
		} else {
			// Changed commits
			hdr := NewRangeDiffHeader(entryA.Patch, entryB.Patch, entryB.Matching+1, entryA.Matching+1)
			files := filesChanged(entryA.Patch, entryB.Patch)
			outputs = append(outputs, &RangeDiffOutput{
				Order:  entryA.Matching + 1,
				Header: hdr,
				Files:  files,
				Type:   "changed",
			})
		}
	}

	sort.Slice(outputs, func(i, j int) bool {
		return outputs[i].Order < outputs[j].Order
	})
	return outputs
}

// fileContent extracts the diff content from a file for comparison.
func fileContent(f *gitdiff.File) string {
	var buf strings.Builder
	for _, frag := range f.TextFragments {
		for _, line := range frag.Lines {
			buf.WriteString(line.String())
		}
	}
	return buf.String()
}

// filesAdded returns a list of files added in the given patch.
func filesAdded(p *Patch) []*RangeDiffFile {
	files := []*RangeDiffFile{}
	for _, f := range p.Files {
		files = append(files, &RangeDiffFile{
			NewName: f.NewName,
			OldName: f.OldName,
			Type:    "added",
		})
	}
	return files
}

// filesRemoved returns a list of files removed from the given patch.
func filesRemoved(p *Patch) []*RangeDiffFile {
	files := []*RangeDiffFile{}
	for _, f := range p.Files {
		files = append(files, &RangeDiffFile{
			NewName: f.NewName,
			OldName: f.OldName,
			Type:    "removed",
		})
	}
	return files
}

// filesChanged returns a list of files that were added, removed, or changed
// between two matched patches.
func filesChanged(oldPatch, newPatch *Patch) []*RangeDiffFile {
	files := []*RangeDiffFile{}

	// Build lookup maps by new file name
	oldFiles := map[string]*gitdiff.File{}
	for _, f := range oldPatch.Files {
		oldFiles[f.NewName] = f
	}
	newFiles := map[string]*gitdiff.File{}
	for _, f := range newPatch.Files {
		newFiles[f.NewName] = f
	}

	// Find changed and removed files
	for name, oldFile := range oldFiles {
		newFile, ok := newFiles[name]
		if !ok {
			// File removed
			files = append(files, &RangeDiffFile{
				OldName: oldFile.OldName,
				Type:    "removed",
			})
		} else if fileContent(oldFile) != fileContent(newFile) {
			// File changed
			files = append(files, &RangeDiffFile{
				OldName: oldFile.OldName,
				NewName: newFile.NewName,
				Type:    "changed",
			})
		}
	}

	// Find added files
	for name, newFile := range newFiles {
		if _, ok := oldFiles[name]; !ok {
			files = append(files, &RangeDiffFile{
				NewName: newFile.NewName,
				OldName: newFile.OldName,
				Type:    "added",
			})
		}
	}

	// Sort for deterministic output
	sort.Slice(files, func(i, j int) bool {
		return files[i].NewName < files[j].NewName
	})
	return files
}

// RangeDiffToStr returns a simple string representation of the range diff.
func RangeDiffToStr(diffs []*RangeDiffOutput) string {
	out := ""
	for _, diff := range diffs {
		out += diff.Header.String()
		for _, f := range diff.Files {
			name := f.NewName
			if name == "" {
				name = f.OldName
			}
			switch f.Type {
			case "added":
				out += "  + " + name + "\n"
			case "removed":
				out += "  - " + name + "\n"
			case "changed":
				out += "  ~ " + name + "\n"
			}
		}
	}
	return out
}

// --- Matching algorithm (unchanged) ---

func findExactMatches(a, b []*patchEntry) {
	for i, entryA := range a {
		for j, entryB := range b {
			if entryA.ContentSha == entryB.ContentSha {
				a[i].Matching = j
				b[j].Matching = i
			}
		}
	}
}

func createMatrix(rows, cols int) [][]int {
	mat := make([][]int, rows)
	for i := range mat {
		mat[i] = make([]int, cols)
	}
	return mat
}

func getCorrespondences(a, b []*patchEntry, creationFactor int) {
	n := len(a) + len(b)
	cost := createMatrix(n, n)

	for i, entryA := range a {
		for j, entryB := range b {
			var c int
			if entryA.Matching == j {
				c = 0
			} else if entryA.Matching == -1 && entryB.Matching == -1 {
				c = absDiff(entryA.Size, entryB.Size)
			} else {
				c = COST_MAX
			}
			cost[i][j] = c
		}
	}

	for j, entryB := range b {
		creationCost := (entryB.Size * creationFactor) / 100
		if entryB.Matching >= 0 {
			creationCost = math.MaxInt32
		}
		for i := len(a); i < n; i++ {
			cost[i][j] = creationCost
		}
	}

	for i := len(a); i < n; i++ {
		for j := len(b); j < n; j++ {
			cost[i][j] = 0
		}
	}

	assignment, _ := ha.Solve(cost)
	for i := range a {
		j := assignment[i]
		if j >= 0 && j < len(b) {
			a[i].Matching = j
			b[j].Matching = i
		}
	}
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
