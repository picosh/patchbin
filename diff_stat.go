package patchbin

import (
	"fmt"
	"math"
	"strings"
)

const diffStatMaxBarWidth = 30

// DiffStatFile represents a single file's line in a git-diff --stat summary.
type DiffStatFile struct {
	DisplayName string
	FileAnchor  string
	Adds        int64
	Dels        int64
	Total       int64
	IsBinary    bool
	PlusBar     string
	MinusBar    string
}

// DiffStat holds the git-diff --stat style summary for a patch.
type DiffStat struct {
	Files        []DiffStatFile
	FilesChanged int
	TotalAdds    int64
	TotalDels    int64
	SummaryText  string
}

// HasFiles reports whether the diff stat has any files to display.
func (d DiffStat) HasFiles() bool {
	return len(d.Files) > 0
}

// ComputeDiffStat builds a git-diff --stat style summary from parsed patch files.
func ComputeDiffStat(patchFiles []*PatchFile) DiffStat {
	stat := DiffStat{
		Files:        make([]DiffStatFile, 0, len(patchFiles)),
		FilesChanged: len(patchFiles),
	}
	if len(patchFiles) == 0 {
		return stat
	}

	var maxTotal int64
	for _, pf := range patchFiles {
		stat.TotalAdds += pf.Adds
		stat.TotalDels += pf.Dels
		if !pf.IsBinary {
			if total := pf.Adds + pf.Dels; total > maxTotal {
				maxTotal = total
			}
		}
	}

	for _, pf := range patchFiles {
		name := pf.DisplayName
		if (pf.IsRename || pf.IsCopy) && pf.OldName != "" && pf.NewName != "" && pf.OldName != pf.NewName {
			name = fmt.Sprintf("%s => %s", pf.OldName, pf.NewName)
		}

		total := pf.Adds + pf.Dels
		var plusBar, minusBar string
		if !pf.IsBinary && total > 0 {
			plusCount, minusCount := scaleDiffStatBar(pf.Adds, pf.Dels, maxTotal, diffStatMaxBarWidth)
			plusBar = strings.Repeat("+", plusCount)
			minusBar = strings.Repeat("-", minusCount)
		}

		stat.Files = append(stat.Files, DiffStatFile{
			DisplayName: name,
			FileAnchor:  pf.FileAnchor,
			Adds:        pf.Adds,
			Dels:        pf.Dels,
			Total:       total,
			IsBinary:    pf.IsBinary,
			PlusBar:     plusBar,
			MinusBar:    minusBar,
		})
	}

	stat.SummaryText = formatDiffStatSummary(stat.FilesChanged, stat.TotalAdds, stat.TotalDels)
	return stat
}

func scaleDiffStatBar(adds, dels, maxTotal int64, maxWidth int) (int, int) {
	total := adds + dels
	if total == 0 || maxTotal == 0 {
		return 0, 0
	}
	if maxTotal <= int64(maxWidth) {
		return int(adds), int(dels)
	}

	plusCount := int(math.Round(float64(adds) * float64(maxWidth) / float64(maxTotal)))
	minusCount := int(math.Round(float64(dels) * float64(maxWidth) / float64(maxTotal)))

	// Ensure non-zero additions or deletions always show at least one symbol.
	if adds > 0 && plusCount == 0 {
		plusCount = 1
	}
	if dels > 0 && minusCount == 0 {
		minusCount = 1
	}

	if plusCount+minusCount > maxWidth {
		if plusCount >= minusCount && plusCount > 1 {
			plusCount--
		} else if minusCount > 1 {
			minusCount--
		}
	}

	return plusCount, minusCount
}

func formatDiffStatSummary(filesChanged int, totalAdds, totalDels int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d file", filesChanged)
	if filesChanged != 1 {
		b.WriteByte('s')
	}
	b.WriteString(" changed")

	if totalAdds > 0 {
		fmt.Fprintf(&b, ", %d insertion", totalAdds)
		if totalAdds != 1 {
			b.WriteByte('s')
		}
		b.WriteString("(+)")
	}
	if totalDels > 0 {
		fmt.Fprintf(&b, ", %d deletion", totalDels)
		if totalDels != 1 {
			b.WriteByte('s')
		}
		b.WriteString("(-)")
	}

	return b.String()
}
