package patchbin

import (
	"testing"
)

const sampleDiff = `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,9 @@
 package foo

-func Add(a int) int {
-	return a
+func Add(a int, b int) int {
+	return a + b
+}
+
+func Sub(a, b int) int {
+	return a - b
 }
`

func TestSemanticChangesSmoke(t *testing.T) {
	files, _, err := ParsePatch(sampleDiff)
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
		if c.Name == "Add" && c.Kind == SemanticSignatureChanged {
			sawAdd = true
		}
		if c.Name == "Sub" && c.Kind == SemanticAdded {
			sawSub = true
		}
	}

	if !sawAdd {
		t.Errorf("expected Add to be reported as signature_changed")
	}
	if !sawSub {
		t.Errorf("expected Sub to be reported as added")
	}
}

// bodyOnlyDiff mirrors a hunk deep inside a large function where the
// `func Foo(...) {` line itself isn't part of the fragment's context lines
// -- common in real-world large diffs (e.g. a 400-line function with a
// change on line 200). Only git's hunk-header comment identifies the
// enclosing function; tree-sitter finds no complete declaration node in
// either the old or new fragment text.
const bodyOnlyDiff = `diff --git a/big.go b/big.go
index 1111111..2222222 100644
--- a/big.go
+++ b/big.go
@@ -33,6 +32,6 @@ func testSingleTenantE2E(t *testing.T) {
 	// Hack to wait for startup
 	time.Sleep(time.Millisecond * 100)

-	suite.userKey.MustCmd(suite.patch, "register")
+	suite.userKey.MustCmd(suite.patch, "pr create test")

 	suite.adminKey.MustCmd(suite.patch, "pr create test")
`

func TestSemanticChangesBodyOnlyHunkFallsBackToEnclosingFunc(t *testing.T) {
	files, _, err := ParsePatch(bodyOnlyDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	changes := AnalyzeSemanticChanges(files[0])
	if len(changes) == 0 {
		t.Fatalf("expected a fallback semantic change from the hunk header comment, got none")
	}

	var sawEnclosing bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s hunk=%d", c.Kind, c.EntityKind, c.Name, c.HunkIndex)
		if c.Name == "testSingleTenantE2E" && c.Kind == SemanticModified {
			sawEnclosing = true
		}
	}

	if !sawEnclosing {
		t.Errorf("expected fallback to report testSingleTenantE2E as modified")
	}
}

// closureDiff mirrors a hunk inside an anonymous closure passed as a struct
// field (e.g. cli.Command{Action: func(cCtx *cli.Context) error { ... }}).
// Git's own hunk-header heuristic can't find a nearby "func Name(...)" line
// here either -- it picks up unrelated doc text -- so neither the primary
// extraction nor the enclosing-comment fallback finds a name, and we should
// fall back to a generic line-range chunk instead of reporting nothing.
const closureDiff = `diff --git a/cli.go b/cli.go
index 1111111..2222222 100644
--- a/cli.go
+++ b/cli.go
@@ -239,10 +239,10 @@ To get started, submit a new patch request:
 						}

 						args := cCtx.Args()
-						repoName := "bin"
-						if args.Present() {
-							repoName = args.First()
+						if !args.Present() {
+							return fmt.Errorf("must provide a repo name")
 						}
+						repoName := args.First()

 						body, err := io.ReadAll(sesh)
 						if err != nil {
`

func TestSemanticChangesClosureHunkFallsBackToGenericChunk(t *testing.T) {
	files, _, err := ParsePatch(closureDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	changes := AnalyzeSemanticChanges(files[0])
	if len(changes) == 0 {
		t.Fatalf("expected a fallback generic chunk change, got none")
	}

	var sawChunk bool
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s hunk=%d", c.Kind, c.EntityKind, c.Name, c.HunkIndex)
		if c.EntityKind == "chunk" && c.Kind == SemanticModified {
			sawChunk = true
		}
	}

	if !sawChunk {
		t.Errorf("expected fallback to report a generic chunk change")
	}
}

// multiHunkSameFuncDiff mirrors a large function edited in two separate,
// non-adjacent hunks (e.g. createPrDetail spanning several hundred lines
// with edits near the top and bottom). Each hunk independently falls back
// to reporting the enclosing function, so without deduplication this would
// produce one "modified" entry per hunk instead of one per function.
const multiHunkSameFuncDiff = `diff --git a/handler.go b/handler.go
index 1111111..2222222 100644
--- a/handler.go
+++ b/handler.go
@@ -10,6 +10,6 @@ func createPrDetail(page string) http.HandlerFunc {
 	return func(w http.ResponseWriter, r *http.Request) {
 		id := r.PathValue("id")

-		prID, err := strconv.Atoi(id)
+		prID, convErr := strconv.Atoi(id)
 		if err != nil {
 			w.WriteHeader(http.StatusUnprocessableEntity)
@@ -40,6 +40,6 @@ func createPrDetail(page string) http.HandlerFunc {
 		}

-		logData, err := getLogData(web, pr.ID, aps.Patchsets)
+		logData, logErr := getLogData(web, pr.ID, aps.Patchsets)
 		if err != nil {
 			web.Logger.Error("cannot fetch log data", "err", err)
 		}
`

func TestSemanticChangesDedupesSameEntityAcrossHunks(t *testing.T) {
	files, _, err := ParsePatch(multiHunkSameFuncDiff)
	if err != nil {
		t.Fatalf("ParsePatch: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if len(files[0].TextFragments) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(files[0].TextFragments))
	}

	changes := AnalyzeSemanticChanges(files[0])

	var matches []SemanticChange
	for _, c := range changes {
		t.Logf("change: kind=%s entity=%s name=%s hunk=%d", c.Kind, c.EntityKind, c.Name, c.HunkIndex)
		if c.Name == "createPrDetail" {
			matches = append(matches, c)
		}
	}

	if len(matches) != 1 {
		t.Errorf("expected exactly 1 change for createPrDetail across both hunks, got %d", len(matches))
	}
}
