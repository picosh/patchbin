package patchbin

import (
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

type UserData struct {
	UserID    int64
	Name      string
	IsAdmin   bool
	Pubkey    string
	CreatedAt string
}

type PatchsetData struct {
	*Patchset
	UserData
	FormattedID string
	Date        string
	RangeDiff   []*RangeDiffOutput
}

type PrData struct {
	UserData
	ID    int64
	Slug  string
	Title string
	Date  string
}

type EventLogData struct {
	*EventLog
	UserData
	*Patchset
	FormattedPatchsetID string
	Date                string
	RangeDiff           []*RangeDiffOutput
}

type PatchHunk struct {
	Anchor   string
	DiffText template.HTML
}

type PatchFile struct {
	*gitdiff.File
	DisplayName     string
	FileAnchor      string
	Adds            int64
	Dels            int64
	Hunks           []PatchHunk
	SemanticChanges []SemanticChange
}

// PatchSummary is a lightweight view of a patch used for the commit list,
// without the expensive diff parsing/rendering that a full PatchData needs.
type PatchSummary struct {
	*Patch
	Url                 template.URL
	FormattedAuthorDate string
}

type PatchData struct {
	*Patch
	PatchFiles          []*PatchFile
	PatchHeader         *gitdiff.PatchHeader
	Url                 template.URL
	FormattedAuthorDate string
	SemanticSummary     SemanticSummary
}

type PrDetailData struct {
	Page                string
	RepoName            string
	Branch              string
	Pr                  PrData
	Patchset            *Patchset
	FormattedPatchsetID string
	PatchsetDate        string
	Patches             []PatchSummary
	Patch               *PatchData
	PrevUrl             string
	NextUrl             string
	Logs                []EventLogData
	MetaData
}

type AllPatchData struct {
	Patches   []PatchSummary
	Patchsets []*PatchsetData
}

func getAllPatchData(web *WebCtx, pr *PatchRequest, ps *Patchset) (*AllPatchData, error) {
	patchsets, err := web.Pr.GetPatchsetsByPrID(pr.ID)
	if err != nil {
		return nil, err
	}

	// get patchsets and diff from previous patchset
	patchsetsData := []*PatchsetData{}
	for idx, patchset := range patchsets {
		user, err := web.Pr.GetUserByID(patchset.UserID)
		if err != nil {
			web.Logger.Error("could not get user for patch", "err", err)
			continue
		}

		var prevPatchset *Patchset
		if idx > 0 {
			prevPatchset = patchsets[idx-1]
		}

		var rangeDiff []*RangeDiffOutput
		if idx > 0 {
			rangeDiff, err = web.Pr.DiffPatchsets(prevPatchset, patchset)
			if err != nil {
				web.Logger.Error("could not diff patchset", "err", err)
				continue
			}
		}

		pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
		if err != nil {
			return nil, err
		}

		displayName := web.Backend.ComputeUserName(user.Pubkey)
		data := PatchsetData{
			Patchset:    patchset,
			FormattedID: fmt.Sprintf("%s/%s.%d", pr.RepoName, pr.Slug, idx+1),
			UserData: UserData{
				UserID:    user.ID,
				Name:      displayName,
				IsAdmin:   web.Backend.IsAdmin(pk),
				Pubkey:    user.Pubkey,
				CreatedAt: user.CreatedAt.Format(time.RFC3339),
			},
			Date:      patchset.CreatedAt.Format(time.RFC3339),
			RangeDiff: rangeDiff,
		}
		patchsetsData = append(patchsetsData, &data)
	}

	patchesData := []PatchSummary{}
	if len(patchsetsData) >= 1 {
		psID := ps.ID
		patches, err := web.Pr.GetPatchesByPatchsetID(psID)
		if err != nil {
			return nil, err
		}

		for _, patch := range patches {
			timestamp := patch.AuthorDate.Format(web.Backend.Cfg.TimeFormat)
			patchesData = append(patchesData, PatchSummary{
				Patch:               patch,
				Url:                 template.URL(fmt.Sprintf("patch-%d", patch.ID)),
				FormattedAuthorDate: timestamp,
			})
		}
	}

	return &AllPatchData{
		Patches:   patchesData,
		Patchsets: patchsetsData,
	}, nil
}

func hunkAnchor(patchID int64, fileName string, hunkIdx int) string {
	return fmt.Sprintf("patch-%d-%s-hunk-%d", patchID, fileName, hunkIdx)
}

func getPatchData(web *WebCtx, patch *Patch) (*PatchData, error) {
	diffFiles, preamble, err := ParsePatch(patch.RawText)
	if err != nil {
		return nil, err
	}
	header, err := gitdiff.ParsePatchHeader(preamble)
	if err != nil {
		return nil, err
	}

	patchFiles := []*PatchFile{}
	var semanticSummary SemanticSummary
	for _, file := range diffFiles {
		var adds int64 = 0
		var dels int64 = 0

		fileName := file.NewName
		if fileName == "" {
			fileName = file.OldName
		}

		hunks := make([]PatchHunk, 0, len(file.TextFragments))
		for hunkIdx, frag := range file.TextFragments {
			adds += frag.LinesAdded
			dels += frag.LinesDeleted

			anchor := hunkAnchor(patch.ID, fileName, hunkIdx)
			diffStr, err := FormatDiffHunk(web.Theme, fileName, frag, anchor)
			if err != nil {
				return nil, err
			}

			hunks = append(hunks, PatchHunk{
				Anchor:   anchor,
				DiffText: template.HTML(diffStr),
			})
		}

		semanticChanges := AnalyzeSemanticChanges(file)
		for i := range semanticChanges {
			semanticChanges[i].HunkAnchor = hunkAnchor(patch.ID, fileName, semanticChanges[i].HunkIndex)
		}
		semanticSummary = SummarizeSemanticChanges(semanticSummary, fileName, SupportsSemanticDiff(fileName) && !file.IsBinary, semanticChanges)

		patchFiles = append(patchFiles, &PatchFile{
			File:            file,
			DisplayName:     fileName,
			FileAnchor:      fmt.Sprintf("patch-%d-%s", patch.ID, fileName),
			Adds:            adds,
			Dels:            dels,
			Hunks:           hunks,
			SemanticChanges: semanticChanges,
		})
	}

	timestamp := patch.AuthorDate.Format(web.Backend.Cfg.TimeFormat)
	return &PatchData{
		Patch:               patch,
		Url:                 template.URL(fmt.Sprintf("patch-%d", patch.ID)),
		FormattedAuthorDate: timestamp,
		PatchFiles:          patchFiles,
		PatchHeader:         header,
		SemanticSummary:     semanticSummary,
	}, nil
}

func getLogData(web *WebCtx, prID int64, patchsetsData []*PatchsetData) ([]EventLogData, error) {
	logData := []EventLogData{}
	logs, err := web.Pr.GetEventLogsByPrID(prID)
	if err != nil {
		return logData, err
	}

	slices.SortFunc(logs, func(a *EventLog, b *EventLog) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	for _, eventlog := range logs {
		logUser, _ := web.Pr.GetUserByID(eventlog.UserID)
		pk, err := web.Backend.PubkeyToPublicKey(logUser.Pubkey)
		if err != nil {
			return logData, err
		}
		var logps *Patchset
		var rangeDiff []*RangeDiffOutput
		formattedPsID := ""
		if eventlog.PatchsetID.Int64 > 0 {
			logps, err = web.Pr.GetPatchsetByID(eventlog.PatchsetID.Int64)
			if err != nil {
				web.Logger.Error("cannot get patchset", "err", err, "ps", eventlog.PatchsetID)
				return logData, err
			}
			for _, psData := range patchsetsData {
				if psData.ID == eventlog.PatchsetID.Int64 {
					rangeDiff = psData.RangeDiff
					formattedPsID = psData.FormattedID
					break
				}
			}
		}

		logDisplayName := web.Backend.ComputeUserName(logUser.Pubkey)
		logData = append(logData, EventLogData{
			EventLog:            eventlog,
			FormattedPatchsetID: formattedPsID,
			Patchset:            logps,
			RangeDiff:           rangeDiff,
			UserData: UserData{
				UserID:    logUser.ID,
				Name:      logDisplayName,
				IsAdmin:   web.Backend.IsAdmin(pk),
				Pubkey:    logUser.Pubkey,
				CreatedAt: logUser.CreatedAt.Format(time.RFC3339),
			},
			Date: eventlog.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
		})
	}

	return logData, nil
}

func createPrDetail(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	repo := r.PathValue("repo")
	slugPath := r.PathValue("slug")

	if repo == "prs" {
		redirectLegacyPr(w, r)
		return
	}

	// Repo RSS mode (e.g. /{repo}/rss)
	if slugPath == "rss" {
		repoRssHandler(w, r)
		return
	}

	// 1. Raw patch mode (.patch suffix)
	if strings.HasSuffix(slugPath, ".patch") {
		cleanSlug := strings.TrimSuffix(slugPath, ".patch")
		pr, ps, err := ResolveTarget(web.Pr, fmt.Sprintf("%s:%s", repo, cleanSlug))
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		patches, err := web.Pr.GetPatchesByPatchsetID(ps.ID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		events, err := web.Pr.GetEventLogsByPrID(ps.PatchRequestID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		users := resolveUsers(web.Pr, events)
		mbox := GenerateMboxWithCoverLetter(pr, patches, events, users, web.Backend.Cfg.Url)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(mbox))
		return
	}

	// 2. PR RSS mode (/rss suffix)
	if strings.HasSuffix(slugPath, "/rss") {
		cleanSlug := strings.TrimSuffix(slugPath, "/rss")
		pr, _, err := ResolveTarget(web.Pr, fmt.Sprintf("%s:%s", repo, cleanSlug))
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		renderPrRss(w, r, web, pr)
		return
	}

	// 3. Patch diff navigation (/patches/{patchID})
	var patchID int64
	if idx := strings.Index(slugPath, "/patches/"); idx != -1 {
		patchIDStr := slugPath[idx+len("/patches/"):]
		patchID, _ = strconv.ParseInt(patchIDStr, 10, 64)
		slugPath = slugPath[:idx]
	}

	pr, ps, err := ResolveTarget(web.Pr, fmt.Sprintf("%s:%s", repo, slugPath))
	if err != nil {
		web.Pr.Backend.Logger.Error("cannot resolve target", "err", err, "repo", repo, "slug", slugPath)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	user, err := web.Pr.GetUserByID(pr.UserID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
	if err != nil {
		web.Logger.Error("cannot parse pubkey for pr user", "err", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	isAdmin := web.Backend.IsAdmin(pk)
	displayName := web.Backend.ComputeUserName(user.Pubkey)

	aps, err := getAllPatchData(web, pr, ps)
	if err != nil {
		web.Logger.Error("cannot compute all patch data", "err", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	if len(aps.Patches) == 0 {
		web.Logger.Error("no patches found for patchset", "ps", ps.ID)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	selectedIdx := 0
	if patchID > 0 {
		found := false
		for idx, summary := range aps.Patches {
			if summary.ID == patchID {
				selectedIdx = idx
				found = true
				break
			}
		}
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	} else if patchIDStr := r.PathValue("patchID"); patchIDStr != "" {
		if pID, err := strconv.ParseInt(patchIDStr, 10, 64); err == nil {
			for idx, summary := range aps.Patches {
				if summary.ID == pID {
					selectedIdx = idx
					break
				}
			}
		}
	}

	selectedPatch, err := getPatchData(web, aps.Patches[selectedIdx].Patch)
	if err != nil {
		web.Logger.Error("cannot compute selected patch data", "err", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	rev := getPatchsetRev(web.Pr, ps)
	formattedPsID := fmt.Sprintf("%s/%s", pr.RepoName, pr.Slug)
	if rev > 0 {
		formattedPsID = fmt.Sprintf("%s/%s.%d", pr.RepoName, pr.Slug, rev)
	}

	var prevUrl, nextUrl string
	if selectedIdx > 0 {
		prevUrl = fmt.Sprintf("/%s/patches/%d", formattedPsID, aps.Patches[selectedIdx-1].ID)
	}
	if selectedIdx < len(aps.Patches)-1 {
		nextUrl = fmt.Sprintf("/%s/patches/%d", formattedPsID, aps.Patches[selectedIdx+1].ID)
	}

	logData, err := getLogData(web, pr.ID, aps.Patchsets)
	if err != nil {
		web.Logger.Error("cannot fetch log data", "err", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("content-type", "text/html")
	err = prTmpl.Execute(w, PrDetailData{
		Page:                "pr",
		RepoName:            pr.RepoName,
		Branch:              "main",
		Patchset:            ps,
		FormattedPatchsetID: formattedPsID,
		PatchsetDate:        ps.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
		Patches:             aps.Patches,
		Patch:               selectedPatch,
		PrevUrl:             prevUrl,
		NextUrl:             nextUrl,
		Logs:                logData,
		Pr: PrData{
			ID:   pr.ID,
			Slug: pr.Slug,
			UserData: UserData{
				UserID:    user.ID,
				Name:      displayName,
				IsAdmin:   isAdmin,
				Pubkey:    user.Pubkey,
				CreatedAt: user.CreatedAt.Format(time.RFC3339),
			},
			Title: pr.Name,
			Date:  pr.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
		},
		MetaData: MetaData{
			URL: web.Backend.Cfg.Url,
		},
	})
	if err != nil {
		web.Backend.Logger.Error("cannot execute template", "err", err)
	}
}
