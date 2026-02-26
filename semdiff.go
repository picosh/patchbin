package patchbin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// SemanticChangeKind describes how an entity changed between the old and
// new side of a hunk.
type SemanticChangeKind string

const (
	SemanticAdded            SemanticChangeKind = "added"
	SemanticRemoved          SemanticChangeKind = "removed"
	SemanticModified         SemanticChangeKind = "modified"
	SemanticSignatureChanged SemanticChangeKind = "signature_changed"
	SemanticRenamed          SemanticChangeKind = "renamed"
)

// semanticEntity is a named, queryable unit of code (function, type, etc)
// extracted from one side (old or new) of a single hunk.
type semanticEntity struct {
	Kind      string
	Name      string
	Signature string
	BodyHash  string
}

// SemanticChange is a single reviewer-facing summary line describing what
// changed about one entity in one hunk.
type SemanticChange struct {
	Kind       SemanticChangeKind
	EntityKind string
	Name       string
	OldSig     string
	NewSig     string
	HunkIndex  int
	HunkAnchor string
}

// languageSpec binds a tree-sitter grammar and entity-extraction query to a
// set of file extensions. Adding a new language means adding one of these
// and nothing else in this file.
//
// enclosingNameFromComment extracts an entity name from a unified diff hunk
// header comment (e.g. "func (s *Foo) Bar(...)" -> "Bar"). It's a fallback
// for hunks whose fragment text doesn't include a full declaration node --
// common for hunks that only touch the middle of a large function body,
// since we only have the patch, not the full file, to parse.
type languageSpec struct {
	language                 *sitter.Language
	query                    string
	enclosingNameFromComment func(string) (kind, name string, ok bool)
}

var languageRegistry = map[string]languageSpec{
	".go": {
		language: golang.GetLanguage(),
		query: `
(function_declaration
  name: (identifier) @name) @entity

(method_declaration
  name: (field_identifier) @name) @entity

(type_declaration
  (type_spec name: (type_identifier) @name)) @entity
`,
		enclosingNameFromComment: goEnclosingNameFromComment,
	},
	".js": {
		language:                 javascript.GetLanguage(),
		query:                    jsFamilyQuery,
		enclosingNameFromComment: jsEnclosingNameFromComment,
	},
	".jsx": {
		language:                 javascript.GetLanguage(),
		query:                    jsFamilyQuery,
		enclosingNameFromComment: jsEnclosingNameFromComment,
	},
	".mjs": {
		language:                 javascript.GetLanguage(),
		query:                    jsFamilyQuery,
		enclosingNameFromComment: jsEnclosingNameFromComment,
	},
	".cjs": {
		language:                 javascript.GetLanguage(),
		query:                    jsFamilyQuery,
		enclosingNameFromComment: jsEnclosingNameFromComment,
	},
	".ts": {
		language:                 typescript.GetLanguage(),
		query:                    tsQuery,
		enclosingNameFromComment: jsEnclosingNameFromComment,
	},
	".tsx": {
		language:                 tsx.GetLanguage(),
		query:                    tsQuery,
		enclosingNameFromComment: jsEnclosingNameFromComment,
	},
	".py": {
		language: python.GetLanguage(),
		query: `
(function_definition
  name: (identifier) @name) @entity

(class_definition
  name: (identifier) @name) @entity
`,
		enclosingNameFromComment: pyEnclosingNameFromComment,
	},
	".rs": {
		language: rust.GetLanguage(),
		query: `
(function_item
  name: (identifier) @name) @entity

(struct_item
  name: (type_identifier) @name) @entity

(enum_item
  name: (type_identifier) @name) @entity

(trait_item
  name: (type_identifier) @name) @entity
`,
		enclosingNameFromComment: rustEnclosingNameFromComment,
	},
}

// jsFamilyQuery covers the declaration shapes shared by JavaScript and
// TypeScript.
const jsFamilyQuery = `
(function_declaration
  name: (identifier) @name) @entity

(method_definition
  name: (property_identifier) @name) @entity

(class_declaration
  name: (identifier) @name) @entity
`

// tsQuery covers TypeScript's declaration shapes. It can't share
// jsFamilyQuery's class_declaration pattern because TypeScript's grammar
// requires a (type_identifier) name node there instead of JavaScript's
// (identifier), and a query naming a field type invalid for the grammar
// fails to compile at all, not just to match.
const tsQuery = `
(function_declaration
  name: (identifier) @name) @entity

(method_definition
  name: (property_identifier) @name) @entity

(class_declaration
  name: (type_identifier) @name) @entity

(interface_declaration
  name: (type_identifier) @name) @entity

(type_alias_declaration
  name: (type_identifier) @name) @entity
`

var goFuncCommentPattern = regexp.MustCompile(`^func\s*(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

func goEnclosingNameFromComment(comment string) (kind, name string, ok bool) {
	m := goFuncCommentPattern.FindStringSubmatch(comment)
	if m == nil {
		return "", "", false
	}
	return "function_declaration", m[1], true
}

var (
	jsFunctionCommentPattern = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	jsClassCommentPattern    = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsMethodCommentPattern   = regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+|async\s+|readonly\s+)*(?:get\s+|set\s+)?([A-Za-z_$][A-Za-z0-9_$]*)\s*(?:<[^>]*>)?\s*\(`)
	jsControlKeywords        = map[string]bool{
		"if": true, "for": true, "while": true, "switch": true, "catch": true,
		"function": true, "return": true, "constructor": true,
	}
)

func jsEnclosingNameFromComment(comment string) (kind, name string, ok bool) {
	if m := jsFunctionCommentPattern.FindStringSubmatch(comment); m != nil {
		return "function_declaration", m[1], true
	}
	if m := jsClassCommentPattern.FindStringSubmatch(comment); m != nil {
		return "class_declaration", m[1], true
	}
	if m := jsMethodCommentPattern.FindStringSubmatch(comment); m != nil && !jsControlKeywords[m[1]] {
		return "method_definition", m[1], true
	}
	return "", "", false
}

var (
	pyFunctionCommentPattern = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pyClassCommentPattern    = regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

func pyEnclosingNameFromComment(comment string) (kind, name string, ok bool) {
	if m := pyFunctionCommentPattern.FindStringSubmatch(comment); m != nil {
		return "function_definition", m[1], true
	}
	if m := pyClassCommentPattern.FindStringSubmatch(comment); m != nil {
		return "class_definition", m[1], true
	}
	return "", "", false
}

var (
	rustFunctionCommentPattern = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?(?:extern\s+"[^"]*"\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustStructCommentPattern   = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?struct\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustEnumCommentPattern     = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?enum\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustTraitCommentPattern    = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?trait\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

func rustEnclosingNameFromComment(comment string) (kind, name string, ok bool) {
	if m := rustFunctionCommentPattern.FindStringSubmatch(comment); m != nil {
		return "function_item", m[1], true
	}
	if m := rustStructCommentPattern.FindStringSubmatch(comment); m != nil {
		return "struct_item", m[1], true
	}
	if m := rustEnumCommentPattern.FindStringSubmatch(comment); m != nil {
		return "enum_item", m[1], true
	}
	if m := rustTraitCommentPattern.FindStringSubmatch(comment); m != nil {
		return "trait_item", m[1], true
	}
	return "", "", false
}

func languageForFile(name string) (languageSpec, bool) {
	spec, ok := languageRegistry[strings.ToLower(filepath.Ext(name))]
	return spec, ok
}

// SupportsSemanticDiff reports whether a file name's extension has a
// registered language spec, i.e. whether AnalyzeSemanticChanges can produce
// anything better than an empty result for it.
func SupportsSemanticDiff(fileName string) bool {
	_, ok := languageForFile(fileName)
	return ok
}

// SemanticSummary aggregates semantic changes across every file in a patch,
// for a reviewer-facing rollup shown above the per-file breakdown.
type SemanticSummary struct {
	Added             int
	Modified          int
	SignatureChanged  int
	Removed           int
	AnalyzedFileCount int
	SkippedFiles      []string
}

func (s SemanticSummary) HasContent() bool {
	return s.AnalyzedFileCount > 0 || len(s.SkippedFiles) > 0
}

func (s SemanticSummary) Total() int {
	return s.Added + s.Modified + s.SignatureChanged + s.Removed
}

// SummarizeSemanticChanges folds one file's changes into a running summary.
// Call once per file in a patch with its changes (possibly nil) and whether
// the file's language was supported, then use the returned summary as-is.
func SummarizeSemanticChanges(summary SemanticSummary, fileName string, supported bool, changes []SemanticChange) SemanticSummary {
	if !supported {
		summary.SkippedFiles = append(summary.SkippedFiles, fileName)
		return summary
	}

	summary.AnalyzedFileCount++
	for _, c := range changes {
		switch c.Kind {
		case SemanticAdded:
			summary.Added++
		case SemanticRemoved:
			summary.Removed++
		case SemanticSignatureChanged:
			summary.SignatureChanged++
		default:
			summary.Modified++
		}
	}

	return summary
}

// AnalyzeSemanticChanges produces a reviewer-facing list of semantic changes
// for a single diffed file. It only has access to the hunks present in the
// patch, not the full pre/post-image files, so entity extraction runs
// per-hunk on the old and new fragment text. Unsupported languages or parse
// failures degrade to an empty, non-error result so callers can always fall
// back to the line diff.
func AnalyzeSemanticChanges(file *gitdiff.File) []SemanticChange {
	name := file.NewName
	if name == "" {
		name = file.OldName
	}

	spec, ok := languageForFile(name)
	if !ok || file.IsBinary {
		return nil
	}

	query, err := sitter.NewQuery([]byte(spec.query), spec.language)
	if err != nil {
		return nil
	}
	defer query.Close()

	var changes []SemanticChange
	for hunkIdx, frag := range file.TextFragments {
		oldText, newText := fragmentSides(frag)

		oldEntities := extractEntities(spec.language, query, oldText)
		newEntities := extractEntities(spec.language, query, newText)

		hunkChanges := diffEntities(oldEntities, newEntities, hunkIdx)
		if len(hunkChanges) == 0 {
			hunkChanges = enclosingChangeFromComment(spec, frag, hunkIdx)
		}
		if len(hunkChanges) == 0 {
			hunkChanges = genericChunkChange(frag, hunkIdx)
		}
		changes = append(changes, hunkChanges...)
	}

	return mergeChangesByEntity(changes)
}

// semanticChangeKindRank orders SemanticChangeKind by specificity, most
// specific first, so mergeChangesByEntity can keep the most informative
// classification when the same entity is flagged by more than one hunk.
var semanticChangeKindRank = map[SemanticChangeKind]int{
	SemanticSignatureChanged: 0,
	SemanticRenamed:          1,
	SemanticAdded:            2,
	SemanticRemoved:          2,
	SemanticModified:         3,
}

// mergeChangesByEntity collapses multiple hunks flagging the same entity
// (e.g. a function whose body spans several hunks) into a single change.
// A large function edited across many hunks would otherwise produce one
// "modified" entry per hunk that touches it, repeating the same information
// with no added value. The first hunk's anchor is kept for the link, but the
// most specific kind across all matching hunks wins.
func mergeChangesByEntity(changes []SemanticChange) []SemanticChange {
	order := make([]string, 0, len(changes))
	merged := make(map[string]SemanticChange, len(changes))

	for _, c := range changes {
		key := c.EntityKind + "\x00" + c.Name
		existing, ok := merged[key]
		if !ok {
			merged[key] = c
			order = append(order, key)
			continue
		}
		if semanticChangeKindRank[c.Kind] < semanticChangeKindRank[existing.Kind] {
			existing.Kind = c.Kind
			existing.OldSig = c.OldSig
			existing.NewSig = c.NewSig
			merged[key] = existing
		}
	}

	result := make([]SemanticChange, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	return result
}

// enclosingChangeFromComment falls back to git's own "nearest enclosing
// function" hunk header (gitdiff.TextFragment.Comment) when a hunk's
// fragment text doesn't contain a full declaration for tree-sitter to
// match -- typically because the hunk only touches lines deep inside a
// function body, and we don't have the full file to parse for context.
func enclosingChangeFromComment(spec languageSpec, frag *gitdiff.TextFragment, hunkIdx int) []SemanticChange {
	if spec.enclosingNameFromComment == nil || frag.Comment == "" {
		return nil
	}
	if frag.LinesAdded == 0 && frag.LinesDeleted == 0 {
		return nil
	}

	kind, name, ok := spec.enclosingNameFromComment(frag.Comment)
	if !ok {
		return nil
	}

	return []SemanticChange{{
		Kind:       SemanticModified,
		EntityKind: kind,
		Name:       name,
		HunkIndex:  hunkIdx,
	}}
}

// genericChunkChange is the last-resort fallback for a hunk with real edits
// where neither a full declaration nor an enclosing-function comment could
// be identified -- e.g. a change inside an anonymous closure passed as a
// struct field, or a hunk in a language/file with no named top-level
// entities (go.mod, go.sum). It reports the hunk by its line range instead
// of by name, so reviewers still see *something* changed there.
func genericChunkChange(frag *gitdiff.TextFragment, hunkIdx int) []SemanticChange {
	if frag.LinesAdded == 0 && frag.LinesDeleted == 0 {
		return nil
	}

	return []SemanticChange{{
		Kind:       SemanticModified,
		EntityKind: "chunk",
		Name:       fmt.Sprintf("lines %d-%d", frag.NewPosition, frag.NewPosition+frag.NewLines-1),
		HunkIndex:  hunkIdx,
	}}
}

// fragmentSides reconstructs the pre-image and post-image text of a hunk
// from its line list, since gitdiff only exposes the unified representation.
func fragmentSides(frag *gitdiff.TextFragment) (oldText, newText string) {
	var oldBuf, newBuf bytes.Buffer
	for _, line := range frag.Lines {
		switch line.Op {
		case gitdiff.OpContext:
			oldBuf.WriteString(line.Line)
			newBuf.WriteString(line.Line)
		case gitdiff.OpDelete:
			oldBuf.WriteString(line.Line)
		case gitdiff.OpAdd:
			newBuf.WriteString(line.Line)
		}
	}
	return oldBuf.String(), newBuf.String()
}

// extractEntities runs the entity query against a best-effort parse of a
// hunk fragment. Tree-sitter is error-tolerant, so a syntactically
// incomplete fragment (a hunk that doesn't span whole declarations) still
// yields partial results rather than failing outright.
func extractEntities(lang *sitter.Language, query *sitter.Query, src string) []semanticEntity {
	if strings.TrimSpace(src) == "" {
		return nil
	}

	root, err := sitter.ParseCtx(context.Background(), []byte(src), lang)
	if err != nil || root == nil {
		return nil
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(query, root)

	srcBytes := []byte(src)
	var entities []semanticEntity
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		var entityNode *sitter.Node
		var name string
		for _, capture := range match.Captures {
			captureName := query.CaptureNameForId(capture.Index)
			switch captureName {
			case "entity":
				entityNode = capture.Node
			case "name":
				name = capture.Node.Content(srcBytes)
			}
		}
		if entityNode == nil || name == "" {
			continue
		}

		body := entityNode.Content(srcBytes)
		entities = append(entities, semanticEntity{
			Kind:      entityNode.Type(),
			Name:      name,
			Signature: signatureOf(body),
			BodyHash:  hashBody(body),
		})
	}

	return entities
}

// signatureOf reduces an entity's source text to a single-line
// approximation of its declaration for display purposes.
func signatureOf(body string) string {
	if idx := strings.Index(body, "{"); idx >= 0 {
		body = body[:idx]
	}
	return strings.Join(strings.Fields(body), " ")
}

func hashBody(body string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(body), " ")))
	return hex.EncodeToString(sum[:])
}

// diffEntities classifies entities found in one hunk's old side vs new side
// by name. It only compares entities within the same hunk since that's the
// unit of context we have available from a patchset alone.
func diffEntities(oldEntities, newEntities []semanticEntity, hunkIdx int) []SemanticChange {
	oldByName := map[string]semanticEntity{}
	for _, e := range oldEntities {
		oldByName[e.Name] = e
	}
	newByName := map[string]semanticEntity{}
	for _, e := range newEntities {
		newByName[e.Name] = e
	}

	var changes []SemanticChange
	for name, newEntity := range newByName {
		oldEntity, existed := oldByName[name]
		if !existed {
			changes = append(changes, SemanticChange{
				Kind:       SemanticAdded,
				EntityKind: newEntity.Kind,
				Name:       name,
				NewSig:     newEntity.Signature,
				HunkIndex:  hunkIdx,
			})
			continue
		}
		if oldEntity.BodyHash == newEntity.BodyHash {
			continue
		}
		kind := SemanticModified
		if oldEntity.Signature != newEntity.Signature {
			kind = SemanticSignatureChanged
		}
		changes = append(changes, SemanticChange{
			Kind:       kind,
			EntityKind: newEntity.Kind,
			Name:       name,
			OldSig:     oldEntity.Signature,
			NewSig:     newEntity.Signature,
			HunkIndex:  hunkIdx,
		})
	}
	for name, oldEntity := range oldByName {
		if _, stillExists := newByName[name]; stillExists {
			continue
		}
		changes = append(changes, SemanticChange{
			Kind:       SemanticRemoved,
			EntityKind: oldEntity.Kind,
			Name:       name,
			OldSig:     oldEntity.Signature,
			HunkIndex:  hunkIdx,
		})
	}

	return changes
}
