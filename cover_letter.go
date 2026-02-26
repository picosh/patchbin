package patchbin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// HasCoverLetter checks if the first patch is a cover letter (no diff).
func HasCoverLetter(patches []*Patch) bool {
	if len(patches) == 0 {
		return false
	}
	return !strings.Contains(patches[0].RawText, "diff --git")
}

// pubkeyFingerprint returns a SHA256 fingerprint for an SSH public key.
func pubkeyFingerprint(pubkey string) string {
	keyBytes := []byte(strings.TrimSpace(pubkey) + "\n")
	hash := sha256.Sum256(keyBytes)
	return "SHA256:" + hex.EncodeToString(hash[:])
}

// patchsetEventTypes are events that represent a new revision being submitted.
var patchsetEventTypes = map[string]bool{
	"pr_patchset_added": true,
	"pr_created":        true,
}

// BuildDiscussion formats event logs into a plain-text discussion thread.
// Uses SSH pubkey fingerprints for user identity.
// Interleaves "Submitted revision ps-X" lines for patchset events.
func BuildDiscussion(events []*EventLog, users map[int64]*User) string {
	if len(events) == 0 {
		return ""
	}

	var buf strings.Builder
	for _, event := range events {
		user := users[event.UserID]
		if user == nil {
			continue
		}

		fp := pubkeyFingerprint(user.Pubkey)
		ts := event.CreatedAt.Format(time.RFC3339)

		// Insert revision marker for patchset events
		if patchsetEventTypes[event.Event] && event.PatchsetID.Valid {
			ps := fmt.Sprintf("ps-%d", event.PatchsetID.Int64)
			fmt.Fprintf(&buf, "[%s] %s:\n", ts, fp)
			fmt.Fprintf(&buf, "  Submitted revision %s\n\n", ps)
		}

		comment := event.Data.Comment
		if comment == "" {
			continue
		}

		fmt.Fprintf(&buf, "[%s] %s:\n", ts, fp)
		// Indent comment lines
		for _, line := range strings.Split(comment, "\n") {
			fmt.Fprintf(&buf, "  %s\n", line)
		}
		buf.WriteString("\n")
	}

	result := buf.String()
	// Strip trailing newline for clean embedding
	return strings.TrimRight(result, "\n")
}

// GenerateCoverLetterPatch creates a cover letter patch in mbox format.
// Empty tree, PR title as subject, References trailer + discussion in body.
func GenerateCoverLetterPatch(pr *PatchRequest, discussion string, cfgURL string) string {
	var buf strings.Builder

	// mbox From line (fake SHA for empty tree commit)
	buf.WriteString("From 0000000000000000000000000000000000000000 Mon Sep 17 00:00:00 2001\n")

	fmt.Fprintf(&buf, "From: patchbin <patchbin@%s>\n", cfgURL)
	fmt.Fprintf(&buf, "Date: %s\n", pr.CreatedAt.Format(time.RFC1123Z))
	fmt.Fprintf(&buf, "Subject: [patchbin #%d] %s\n", pr.ID, pr.Name)
	buf.WriteString("\n")

	// References trailer (in body, before discussion)
	fmt.Fprintf(&buf, "References: https://%s/pr/%d\n", cfgURL, pr.ID)

	// Discussion in commit message body (before any --- separator)
	if discussion != "" {
		buf.WriteString("\n")
		buf.WriteString(discussion)
		buf.WriteString("\n")
	}

	// Sign-off trailer
	buf.WriteString("\n-- \npatchbin cover letter\n")

	return buf.String()
}

// AugmentCoverLetterPatch appends References trailer and discussion to an
// existing cover letter patch. Preserves original content.
func AugmentCoverLetterPatch(rawText string, discussion string, cfgURL string, prID int64) string {
	// Insert References and discussion before the sign-off trailer "-- \n"
	// If no trailer exists, append before the end.

	insert := fmt.Sprintf("\nReferences: https://%s/pr/%d\n", cfgURL, prID)
	if discussion != "" {
		insert += "\n" + discussion + "\n"
	}

	trailer := "\n-- \n"
	idx := strings.Index(rawText, trailer)
	if idx != -1 {
		// Insert before the trailer
		before := rawText[:idx]
		after := rawText[idx:]
		return before + insert + after
	}

	// No trailer found, append at end
	return rawText + insert
}

// GenerateMboxWithCoverLetter returns the full mbox: cover letter + patches.
// If the first patch is already a cover letter, augments it with References + discussion.
// If not, generates a new cover letter from the PR name.
func GenerateMboxWithCoverLetter(pr *PatchRequest, patches []*Patch,
	events []*EventLog, users map[int64]*User, cfgURL string,
) string {
	discussion := BuildDiscussion(events, users)

	var buf strings.Builder

	if HasCoverLetter(patches) {
		// Augment existing cover letter
		augmented := AugmentCoverLetterPatch(patches[0].RawText, discussion, cfgURL, pr.ID)
		buf.WriteString(augmented)

		// Append remaining patches
		for _, patch := range patches[1:] {
			buf.WriteString("\n")
			buf.WriteString(patch.RawText)
		}
	} else {
		// Generate new cover letter
		cover := GenerateCoverLetterPatch(pr, discussion, cfgURL)
		buf.WriteString(cover)

		// Append all patches
		for _, patch := range patches {
			buf.WriteString("\n")
			buf.WriteString(patch.RawText)
		}
	}

	return buf.String()
}
