package patchbin

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"golang.org/x/crypto/ssh"
)

var (
	baseCommitRe = regexp.MustCompile(`base-commit: (.+)\s*`)
	startOfPatch = "From "
	prPrefix     = "pr-"
)

func truncateSha(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}

func GetAuthorizedKeys(pubkeys []string) ([]ssh.PublicKey, error) {
	keys := []ssh.PublicKey{}
	for _, pubkey := range pubkeys {
		if strings.TrimSpace(pubkey) == "" {
			continue
		}
		if strings.HasPrefix(pubkey, "#") {
			continue
		}
		upk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkey))
		if err != nil {
			return keys, err
		}
		keys = append(keys, upk)
	}

	return keys, nil
}

type ParsedID struct {
	PrID int64
	Rev  int // 1-indexed revision number within PR, or 0 if latest
}

func ParseID(raw string) (ParsedID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedID{}, fmt.Errorf("empty ID")
	}

	s := strings.TrimPrefix(raw, "pr-")

	if strings.Contains(s, ".") {
		parts := strings.SplitN(s, ".", 2)
		prID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return ParsedID{}, fmt.Errorf("invalid PR ID in %q", raw)
		}
		revStr := strings.TrimPrefix(parts[1], "v")
		rev, err := strconv.Atoi(revStr)
		if err != nil {
			return ParsedID{}, fmt.Errorf("invalid revision in %q", raw)
		}
		return ParsedID{PrID: prID, Rev: rev}, nil
	}

	prID, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return ParsedID{}, fmt.Errorf("invalid ID format: %s", raw)
	}
	return ParsedID{PrID: prID, Rev: 0}, nil
}

func GetPatchsetByParsedID(gpr GitPatchRequest, parsed ParsedID) (*Patchset, error) {
	patchsets, err := gpr.GetPatchsetsByPrID(parsed.PrID)
	if err != nil {
		return nil, err
	}

	if parsed.Rev == 0 {
		return patchsets[len(patchsets)-1], nil
	}

	if parsed.Rev < 1 || parsed.Rev > len(patchsets) {
		return nil, fmt.Errorf("revision %d does not exist for PR pr-%d (PR has %d revision(s))", parsed.Rev, parsed.PrID, len(patchsets))
	}

	return patchsets[parsed.Rev-1], nil
}

func getPatchsetRev(gpr GitPatchRequest, patchset *Patchset) int {
	if patchset == nil {
		return 0
	}
	patchsets, err := gpr.GetPatchsetsByPrID(patchset.PatchRequestID)
	if err != nil {
		return 0
	}
	for idx, ps := range patchsets {
		if ps.ID == patchset.ID {
			return idx + 1
		}
	}
	return 0
}

func getFormattedPatchsetID(prID int64, rev int) string {
	if prID == 0 || rev == 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d", prID, rev)
}

func getPrID(prID string) (int64, error) {
	recID, err := strconv.Atoi(strings.Replace(prID, prPrefix, "", 1))
	if err != nil {
		return 0, err
	}
	return int64(recID), nil
}

func splitPatchSet(patchset string) []string {
	return strings.Split(patchset, "\n"+startOfPatch)
}

func findBaseCommit(patch string) string {
	strs := baseCommitRe.FindStringSubmatch(patch)
	baseCommit := ""
	if len(strs) > 1 {
		baseCommit = strs[1]
	}
	return baseCommit
}

func patchToDiff(patch io.Reader) (string, error) {
	by, err := io.ReadAll(patch)
	if err != nil {
		return "", err
	}
	str := string(by)
	idx := strings.Index(str, "diff --git")
	if idx == -1 {
		return "", fmt.Errorf("no diff found in patch")
	}
	trailIdx := strings.LastIndex(str, "-- \n")
	if trailIdx >= 0 {
		return str[idx:trailIdx], nil
	}
	return str[idx:], nil
}

func ParsePatch(patchRaw string) ([]*gitdiff.File, string, error) {
	reader := strings.NewReader(patchRaw)
	diffFiles, preamble, err := gitdiff.Parse(reader)
	return diffFiles, preamble, err
}

func ParsePatchset(patchset io.Reader) ([]*Patch, error) {
	patches := []*Patch{}
	buf := new(strings.Builder)
	_, err := io.Copy(buf, patchset)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(buf.String()) == "" {
		return nil, fmt.Errorf("patchset is empty")
	}

	if !strings.HasPrefix(buf.String(), startOfPatch) {
		return nil, fmt.Errorf("unrecognized patchset: must start with %q", startOfPatch)
	}

	patchesRaw := splitPatchSet(buf.String())
	for idx, patchRaw := range patchesRaw {
		patchStr := patchRaw
		if idx > 0 {
			patchStr = startOfPatch + patchRaw
		}
		diffFiles, preamble, err := ParsePatch(patchStr)
		if err != nil {
			return nil, err
		}
		header, err := gitdiff.ParsePatchHeader(preamble)
		if err != nil {
			return nil, err
		}

		baseCommit := findBaseCommit(patchRaw)
		authorName := "Unknown"
		authorEmail := ""
		if header.Author != nil {
			authorName = header.Author.Name
			authorEmail = header.Author.Email
		}

		contentSha := calcContentSha(diffFiles, header)

		patches = append(patches, &Patch{
			AuthorName:    authorName,
			AuthorEmail:   authorEmail,
			AuthorDate:    header.AuthorDate.UTC(),
			Title:         header.Title,
			Body:          header.Body,
			BodyAppendix:  header.BodyAppendix,
			CommitSha:     header.SHA,
			ContentSha:    contentSha,
			RawText:       patchStr,
			BaseCommitSha: sql.NullString{String: baseCommit},
			Files:         diffFiles,
		})
	}

	return patches, nil
}

// calcContentSha calculates a shasum containing the important content
// changes related to a patch.
// We cannot rely on patch.CommitSha because it includes the commit date
// that will change when a user fetches and applies the patch locally.
// We only include +/- lines (not context) so that rebased patches with
// different context lines but identical changes are considered equal.
func calcContentSha(diffFiles []*gitdiff.File, header *gitdiff.PatchHeader) string {
	authorName := ""
	authorEmail := ""
	if header.Author != nil {
		authorName = header.Author.Name
		authorEmail = header.Author.Email
	}
	content := fmt.Sprintf(
		"%s\n%s\n%s\n%s\n",
		header.Title,
		header.Body,
		authorName,
		authorEmail,
	)
	for _, diff := range diffFiles {
		// we need to ignore diffs with base commit because that depends
		// on the client that is exporting the patch
		foundBase := false
		for _, text := range diff.TextFragments {
			for _, line := range text.Lines {
				base := findBaseCommit(line.Line)
				if base != "" {
					foundBase = true
				}
			}
		}

		if foundBase {
			continue
		}

		// Include file names and mode changes, but not OID prefixes since those
		// change when context lines shift (e.g., after rebase)
		dff := fmt.Sprintf(
			"%s->%s %s->%s\n",
			diff.OldName, diff.NewName,
			diff.OldMode.String(), diff.NewMode.String(),
		)
		content += dff

		// Include only added and deleted lines, not context lines.
		// This ensures patches with identical changes but different context
		// (due to rebasing) are considered equal.
		for _, frag := range diff.TextFragments {
			for _, line := range frag.Lines {
				if line.Op == gitdiff.OpAdd || line.Op == gitdiff.OpDelete {
					content += line.String()
				}
			}
		}
	}
	sha := sha256.Sum256([]byte(content))
	shaStr := hex.EncodeToString(sha[:])
	return shaStr
}
