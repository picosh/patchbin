package patchbin

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// buildTestPR returns a minimal PatchRequest for testing.
func buildTestPR(id int64, name string) *PatchRequest {
	return &PatchRequest{
		ID:        id,
		Name:      name,
		RepoName:  "test-repo",
		Status:    StatusOpen,
		CreatedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}
}

// buildTestUser returns a minimal User for testing.
func buildTestUser(id int64, pubkey string) *User {
	return &User{
		ID:     id,
		Pubkey: pubkey,
	}
}

// buildTestEvents returns event logs for testing.
func buildTestEvents() []*EventLog {
	return []*EventLog{
		{
			ID:             1,
			UserID:         1,
			PatchRequestID: sql.NullInt64{Int64: 42, Valid: true},
			Event:          "pr_created",
			CreatedAt:      time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			Data:           EventData{Comment: "Initial submission."},
		},
		{
			ID:             2,
			UserID:         2,
			PatchRequestID: sql.NullInt64{Int64: 42, Valid: true},
			PatchsetID:     sql.NullInt64{Int64: 3, Valid: true},
			Event:          "pr_patchset_added",
			CreatedAt:      time.Date(2025, 1, 16, 9, 0, 0, 0, time.UTC),
			Data:           EventData{Comment: "LGTM. One suggestion: add rate limiting."},
		},
	}
}

// buildTestUsers returns a user map for testing.
func buildTestUsers() map[int64]*User {
	return map[int64]*User{
		1: buildTestUser(1, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGtest1 test1@host"),
		2: buildTestUser(2, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGtest2 test2@host"),
	}
}

// buildTestPatchesNoCover returns patches without a cover letter.
func buildTestPatchesNoCover() []*Patch {
	return []*Patch{
		{
			Title:       "feat: add auth middleware",
			Body:        "Adds JWT-based authentication.",
			RawText:     "From abc123 Mon Sep 17 00:00:00 2001\nFrom: Test <test@example.com>\nDate: Wed, 3 Jul 2024 15:18:47 -0400\nSubject: [PATCH] feat: add auth middleware\n\nAdds JWT-based authentication.\n\ndiff --git a/auth.go b/auth.go\n",
			AuthorName:  "Test",
			AuthorEmail: "test@example.com",
		},
	}
}

// buildTestPatchesWithCover returns patches with a user-provided cover letter.
func buildTestPatchesWithCover() []*Patch {
	return []*Patch{
		{
			Title:       "Add torch deps",
			Body:        "I took the liberty of adding a requirements file for python.\n\nBob Sour (1):\n  chore: add torch to requirements",
			RawText:     "From def456 Mon Sep 17 00:00:00 2001\nFrom: Bob <bob@example.com>\nDate: Sun, 14 Jul 2024 07:14:44 -0400\nSubject: [PATCH 0/2] Add torch deps\n\nI took the liberty of adding a requirements file for python.\n\nBob Sour (1):\n  chore: add torch to requirements\n\n-- \n2.45.2\n",
			AuthorName:  "Bob",
			AuthorEmail: "bob@example.com",
		},
		{
			Title:       "feat: build an rnn",
			Body:        "Build a simple RNN.",
			RawText:     "From abc123 Mon Sep 17 00:00:00 2001\nFrom: Bob <bob@example.com>\nDate: Wed, 3 Jul 2024 15:18:47 -0400\nSubject: [PATCH 1/2] feat: build an rnn\n\nBuild a simple RNN.\n\ndiff --git a/train.py b/train.py\n",
			AuthorName:  "Bob",
			AuthorEmail: "bob@example.com",
		},
	}
}

func TestHasCoverLetter_NoCover(t *testing.T) {
	patches := buildTestPatchesNoCover()
	if HasCoverLetter(patches) {
		t.Fatal("expected no cover letter, got true")
	}
}

func TestHasCoverLetter_WithCover(t *testing.T) {
	patches := buildTestPatchesWithCover()
	if !HasCoverLetter(patches) {
		t.Fatal("expected cover letter, got false")
	}
}

func TestHasCoverLetter_EmptyPatches(t *testing.T) {
	if HasCoverLetter(nil) {
		t.Fatal("expected no cover letter for nil patches")
	}
	if HasCoverLetter([]*Patch{}) {
		t.Fatal("expected no cover letter for empty patches")
	}
}

func TestBuildDiscussion(t *testing.T) {
	events := buildTestEvents()
	users := buildTestUsers()

	discussion := BuildDiscussion(events, users)

	if discussion == "" {
		t.Fatal("discussion should not be empty")
	}

	// Should contain pubkey fingerprints, not usernames
	if !strings.Contains(discussion, "SHA256:") {
		t.Fatal("discussion should contain SHA256 pubkey fingerprints")
	}

	// Should contain event comments
	if !strings.Contains(discussion, "Initial submission") {
		t.Fatal("discussion should contain first event comment")
	}
	if !strings.Contains(discussion, "rate limiting") {
		t.Fatal("discussion should contain feedback comment")
	}

	// Should contain timestamps
	if !strings.Contains(discussion, "2025-01-15") {
		t.Fatal("discussion should contain date")
	}

	// Should contain revision markers for patchset events
	if !strings.Contains(discussion, "Submitted revision ps-3") {
		t.Fatalf("discussion should contain revision marker, got:\n%s", discussion)
	}
	// Revision marker should have pubkey fingerprint
	if !strings.Contains(discussion, "SHA256:") {
		t.Fatal("revision marker should include SHA256 fingerprint")
	}
}

func TestBuildDiscussion_RevisionMarkers(t *testing.T) {
	events := []*EventLog{
		{
			ID:             1,
			UserID:         1,
			PatchRequestID: sql.NullInt64{Int64: 1, Valid: true},
			Event:          "pr_created",
			CreatedAt:      time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			Data:           EventData{Comment: "Initial submission."},
		},
		{
			ID:             2,
			UserID:         1,
			PatchRequestID: sql.NullInt64{Int64: 1, Valid: true},
			PatchsetID:     sql.NullInt64{Int64: 3, Valid: true},
			Event:          "pr_patchset_added",
			CreatedAt:      time.Date(2025, 1, 16, 9, 0, 0, 0, time.UTC),
			Data:           EventData{Comment: "Updated based on feedback."},
		},
		{
			ID:             3,
			UserID:         2,
			PatchRequestID: sql.NullInt64{Int64: 1, Valid: true},
			PatchsetID:     sql.NullInt64{Int64: 5, Valid: true},
			Event:          "pr_patchset_added",
			CreatedAt:      time.Date(2025, 1, 17, 11, 0, 0, 0, time.UTC),
			Data:           EventData{Comment: "LGTM."},
		},
	}
	users := map[int64]*User{
		1: buildTestUser(1, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGtest1 test1@host"),
		2: buildTestUser(2, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGtest2 test2@host"),
	}

	discussion := BuildDiscussion(events, users)

	// Should have revision markers interleaved with comments, same format as comments
	if !strings.Contains(discussion, "Submitted revision ps-3") {
		t.Fatal("should contain ps-3 revision marker")
	}
	if !strings.Contains(discussion, "Submitted revision ps-5") {
		t.Fatal("should contain ps-5 revision marker")
	}

	// Revision markers should come before associated comments
	ps3Idx := strings.Index(discussion, "Submitted revision ps-3")
	updatedIdx := strings.Index(discussion, "Updated based on feedback")
	if ps3Idx >= updatedIdx {
		t.Fatal("revision marker should come before its comment")
	}
}

func TestBuildDiscussion_EmptyEvents(t *testing.T) {
	discussion := BuildDiscussion(nil, buildTestUsers())
	if discussion != "" {
		t.Fatalf("expected empty discussion for no events, got: %q", discussion)
	}
}

func TestGenerateCoverLetterPatch(t *testing.T) {
	pr := buildTestPR(42, "feat: add auth middleware")
	discussion := "[2025-01-15] SHA256:test:\n  Hello world."

	patch := GenerateCoverLetterPatch(pr, discussion, "patchbin.example.com")

	// Should contain PR title in subject
	if !strings.Contains(patch, "feat: add auth middleware") {
		t.Fatal("cover letter should contain PR title in subject")
	}

	// Should contain References trailer with URL
	expectedRef := "References: https://patchbin.example.com/pr/42"
	if !strings.Contains(patch, expectedRef) {
		t.Fatalf("cover letter should contain References trailer, got:\n%s", patch)
	}

	// Should contain discussion
	if !strings.Contains(patch, "Hello world.") {
		t.Fatal("cover letter should contain discussion")
	}

	// Should be a valid mbox (starts with "From ")
	if !strings.HasPrefix(patch, "From ") {
		t.Fatal("cover letter should start with 'From ' (mbox format)")
	}

	// Should NOT contain diff --git (empty tree)
	if strings.Contains(patch, "diff --git") {
		t.Fatal("cover letter should not contain diffs (empty tree)")
	}

	// Discussion should be BEFORE any --- separator (in commit message body)
	sepIdx := strings.Index(patch, "\n---\n")
	discIdx := strings.Index(patch, "Hello world.")
	if sepIdx != -1 && discIdx > sepIdx {
		t.Fatal("discussion should be before the --- separator (in commit message body)")
	}
}

func TestGenerateCoverLetterPatch_NoDiscussion(t *testing.T) {
	pr := buildTestPR(1, "fix: typo")
	patch := GenerateCoverLetterPatch(pr, "", "patchbin.example.com")

	if !strings.Contains(patch, "fix: typo") {
		t.Fatal("cover letter should contain PR title")
	}
	if !strings.Contains(patch, "References:") {
		t.Fatal("cover letter should contain References trailer")
	}
}

func TestAugmentCoverLetterPatch(t *testing.T) {
	original := "From def456 Mon Sep 17 00:00:00 2001\nFrom: Bob <bob@example.com>\nDate: Sun, 14 Jul 2024 07:14:44 -0400\nSubject: [PATCH 0/2] Add torch deps\n\nI took the liberty of adding a requirements file.\n\n-- \n2.45.2\n"
	discussion := "[2025-01-15] SHA256:test:\n  Great patch!"

	augmented := AugmentCoverLetterPatch(original, discussion, "patchbin.example.com", 42)

	// Should preserve original content
	if !strings.Contains(augmented, "Add torch deps") {
		t.Fatal("augmented cover letter should preserve original subject")
	}
	if !strings.Contains(augmented, "I took the liberty") {
		t.Fatal("augmented cover letter should preserve original body")
	}

	// Should add References trailer
	if !strings.Contains(augmented, "References: https://patchbin.example.com/pr/42") {
		t.Fatal("augmented cover letter should contain References trailer")
	}

	// Should add discussion
	if !strings.Contains(augmented, "Great patch!") {
		t.Fatal("augmented cover letter should contain discussion")
	}

	// Should still be valid mbox
	if !strings.HasPrefix(augmented, "From ") {
		t.Fatal("augmented cover letter should start with 'From ' (mbox format)")
	}
}

func TestAugmentCoverLetterPatch_NoDiscussion(t *testing.T) {
	original := "From def456 Mon Sep 17 00:00:00 2001\nFrom: Bob <bob@example.com>\nDate: Sun, 14 Jul 2024 07:14:44 -0400\nSubject: [PATCH 0/1] Simple patch\n\nJust a patch.\n\n-- \n2.45.2\n"

	augmented := AugmentCoverLetterPatch(original, "", "patchbin.example.com", 1)

	// Should preserve original content
	if !strings.Contains(augmented, "Simple patch") {
		t.Fatal("augmented cover letter should preserve original subject")
	}
	if !strings.Contains(augmented, "Just a patch.") {
		t.Fatal("augmented cover letter should preserve original body")
	}

	// Should still add References
	if !strings.Contains(augmented, "References:") {
		t.Fatal("augmented cover letter should contain References trailer")
	}
}

func TestGenerateMboxWithCoverLetter_NoExistingCover(t *testing.T) {
	pr := buildTestPR(42, "feat: add auth middleware")
	patches := buildTestPatchesNoCover()
	events := buildTestEvents()
	users := buildTestUsers()

	mbox := GenerateMboxWithCoverLetter(pr, patches, events, users, "patchbin.example.com")

	// Should start with a cover letter (From ... for the cover)
	if !strings.HasPrefix(mbox, "From ") {
		t.Fatal("mbox should start with 'From ' (cover letter)")
	}

	// Should contain cover letter with PR title
	if !strings.Contains(mbox, "[patchbin #42] feat: add auth middleware") {
		t.Fatal("mbox should contain cover letter with PR title")
	}

	// Should contain References
	if !strings.Contains(mbox, "References: https://patchbin.example.com/pr/42") {
		t.Fatal("mbox should contain References trailer")
	}

	// Should contain discussion with pubkey fingerprints
	if !strings.Contains(mbox, "SHA256:") {
		t.Fatal("mbox should contain discussion with SHA256 fingerprints")
	}

	// Should contain the original patches
	if !strings.Contains(mbox, "feat: add auth middleware") {
		t.Fatal("mbox should contain original patches")
	}

	// Should contain the diff from original patches
	if !strings.Contains(mbox, "diff --git") {
		t.Fatal("mbox should contain diffs from original patches")
	}
}

func TestGenerateMboxWithCoverLetter_WithExistingCover(t *testing.T) {
	pr := buildTestPR(7, "Add torch deps")
	patches := buildTestPatchesWithCover()
	events := buildTestEvents()
	users := buildTestUsers()

	mbox := GenerateMboxWithCoverLetter(pr, patches, events, users, "patchbin.example.com")

	// Should preserve the user's cover letter content
	if !strings.Contains(mbox, "I took the liberty") {
		t.Fatal("mbox should preserve user's cover letter body")
	}

	// Should add References to the cover letter
	if !strings.Contains(mbox, "References: https://patchbin.example.com/pr/7") {
		t.Fatal("mbox should add References trailer to existing cover letter")
	}

	// Should add discussion
	if !strings.Contains(mbox, "SHA256:") {
		t.Fatal("mbox should contain discussion with SHA256 fingerprints")
	}

	// Should contain all original patches
	if !strings.Contains(mbox, "feat: build an rnn") {
		t.Fatal("mbox should contain original patches")
	}
}

func TestGenerateMboxWithCoverLetter_NoEvents(t *testing.T) {
	pr := buildTestPR(1, "fix: typo")
	patches := buildTestPatchesNoCover()

	mbox := GenerateMboxWithCoverLetter(pr, patches, nil, nil, "patchbin.example.com")

	// Should still have a cover letter
	if !strings.Contains(mbox, "[patchbin #1] fix: typo") {
		t.Fatal("mbox should contain cover letter with PR title")
	}

	// Should still have References
	if !strings.Contains(mbox, "References:") {
		t.Fatal("mbox should contain References trailer")
	}

	// Should contain the original patches
	if !strings.Contains(mbox, "diff --git") {
		t.Fatal("mbox should contain diffs from original patches")
	}
}

func TestGenerateMboxWithCoverLetter_PreservesPatchOrder(t *testing.T) {
	pr := buildTestPR(10, "multi-patch series")
	patches := []*Patch{
		{
			Title:   "first commit",
			RawText: "From aaa Mon Sep 17 00:00:00 2001\nFrom: A <a@b.com>\nSubject: [PATCH 1/3] first commit\n\ndiff --git a/a.go b/a.go\n",
		},
		{
			Title:   "second commit",
			RawText: "From bbb Mon Sep 17 00:00:00 2001\nFrom: A <a@b.com>\nSubject: [PATCH 2/3] second commit\n\ndiff --git a/b.go b/b.go\n",
		},
		{
			Title:   "third commit",
			RawText: "From ccc Mon Sep 17 00:00:00 2001\nFrom: A <a@b.com>\nSubject: [PATCH 3/3] third commit\n\ndiff --git a/c.go b/c.go\n",
		},
	}

	mbox := GenerateMboxWithCoverLetter(pr, patches, nil, nil, "patchbin.example.com")

	// Verify patch order is preserved
	firstIdx := strings.Index(mbox, "first commit")
	secondIdx := strings.Index(mbox, "second commit")
	thirdIdx := strings.Index(mbox, "third commit")

	if firstIdx >= secondIdx || secondIdx >= thirdIdx {
		t.Fatalf("patch order not preserved: first=%d, second=%d, third=%d", firstIdx, secondIdx, thirdIdx)
	}
}
