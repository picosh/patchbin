package patchbin

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	formatterHtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

var inlineChromaFormatter = formatterHtml.New(
	formatterHtml.WithClasses(true),
	formatterHtml.PreventSurroundingPre(true),
)

func resolveLexer(fileName string) chroma.Lexer {
	lexer := lexers.Match(fileName)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	return chroma.Coalesce(lexer)
}

// FormatDiffHunk formats a gitdiff.TextFragment as a syntax-highlighted HTML table
// with diff overlay classes and clickable anchor links on line numbers.
func FormatDiffHunk(theme *chroma.Style, fileName string, frag *gitdiff.TextFragment, hunkAnchor string) (string, error) {
	if theme == nil {
		theme = styles.Fallback
	}
	lexer := resolveLexer(fileName)

	var buf bytes.Buffer
	buf.WriteString("<div class=\"chroma diff-container\"><table class=\"diff-table\"><tbody class=\"diff-hunk\">")

	// Hunk header row
	hunkHeader := strings.TrimSpace(frag.Header())
	if hunkHeader == "" {
		hunkHeader = fmt.Sprintf("@@ -%d,%d +%d,%d @@", frag.OldPosition, frag.OldLines, frag.NewPosition, frag.NewLines)
		if frag.Comment != "" {
			hunkHeader += " " + frag.Comment
		}
	}

	buf.WriteString("<tr class=\"diff-line diff-line-hunk\">")
	buf.WriteString("<td class=\"diff-num diff-num-old\">...</td>")
	buf.WriteString("<td class=\"diff-num diff-num-new\">...</td>")
	buf.WriteString("<td class=\"diff-gutter\"></td>")
	buf.WriteString("<td class=\"diff-code\"><span class=\"gu\">")
	buf.WriteString(html.EscapeString(hunkHeader))
	buf.WriteString("</span></td></tr>\n")

	oldLineNo := frag.OldPosition
	newLineNo := frag.NewPosition

	for _, line := range frag.Lines {
		var oldNumStr, newNumStr, gutter, rowClass string
		var oldAnchorID, newAnchorID string

		switch line.Op {
		case gitdiff.OpContext:
			oldNumStr = strconv.FormatInt(oldLineNo, 10)
			newNumStr = strconv.FormatInt(newLineNo, 10)
			if hunkAnchor != "" {
				oldAnchorID = fmt.Sprintf("%s-L%s", hunkAnchor, oldNumStr)
				newAnchorID = fmt.Sprintf("%s-R%s", hunkAnchor, newNumStr)
			}
			oldLineNo++
			newLineNo++
			gutter = " "
			rowClass = "diff-line diff-line-context"
		case gitdiff.OpDelete:
			oldNumStr = strconv.FormatInt(oldLineNo, 10)
			newNumStr = ""
			if hunkAnchor != "" {
				oldAnchorID = fmt.Sprintf("%s-L%s", hunkAnchor, oldNumStr)
			}
			oldLineNo++
			gutter = "-"
			rowClass = "diff-line diff-line-delete"
		case gitdiff.OpAdd:
			oldNumStr = ""
			newNumStr = strconv.FormatInt(newLineNo, 10)
			if hunkAnchor != "" {
				newAnchorID = fmt.Sprintf("%s-R%s", hunkAnchor, newNumStr)
			}
			newLineNo++
			gutter = "+"
			rowClass = "diff-line diff-line-add"
		}

		buf.WriteString("<tr class=\"")
		buf.WriteString(rowClass)
		buf.WriteString("\">")

		// Old line number column with anchor link
		buf.WriteString("<td class=\"diff-num diff-num-old\"")
		if oldAnchorID != "" {
			buf.WriteString(" id=\"")
			buf.WriteString(html.EscapeString(oldAnchorID))
			buf.WriteString("\"")
		}
		if oldNumStr != "" {
			buf.WriteString(" data-line-number=\"")
			buf.WriteString(oldNumStr)
			buf.WriteString("\">")
			if oldAnchorID != "" {
				buf.WriteString("<a href=\"#")
				buf.WriteString(html.EscapeString(oldAnchorID))
				buf.WriteString("\">")
				buf.WriteString(oldNumStr)
				buf.WriteString("</a>")
			} else {
				buf.WriteString(oldNumStr)
			}
		} else {
			buf.WriteString(">")
		}
		buf.WriteString("</td>")

		// New line number column with anchor link
		buf.WriteString("<td class=\"diff-num diff-num-new\"")
		if newAnchorID != "" {
			buf.WriteString(" id=\"")
			buf.WriteString(html.EscapeString(newAnchorID))
			buf.WriteString("\"")
		}
		if newNumStr != "" {
			buf.WriteString(" data-line-number=\"")
			buf.WriteString(newNumStr)
			buf.WriteString("\">")
			if newAnchorID != "" {
				buf.WriteString("<a href=\"#")
				buf.WriteString(html.EscapeString(newAnchorID))
				buf.WriteString("\">")
				buf.WriteString(newNumStr)
				buf.WriteString("</a>")
			} else {
				buf.WriteString(newNumStr)
			}
		} else {
			buf.WriteString(">")
		}
		buf.WriteString("</td>")

		// Gutter column
		buf.WriteString("<td class=\"diff-gutter\">")
		buf.WriteString(html.EscapeString(gutter))
		buf.WriteString("</td>")

		// Code cell
		buf.WriteString("<td class=\"diff-code\">")
		lineContent := strings.TrimRight(line.Line, "\r\n")
		if lineContent == "" {
			buf.WriteString("\n")
		} else {
			it, err := lexer.Tokenise(nil, lineContent)
			if err != nil {
				buf.WriteString(html.EscapeString(lineContent))
			} else {
				if err := inlineChromaFormatter.Format(&buf, theme, it); err != nil {
					buf.WriteString(html.EscapeString(lineContent))
				}
			}
		}
		buf.WriteString("</td></tr>\n")
	}

	buf.WriteString("</tbody></table></div>")
	return buf.String(), nil
}
