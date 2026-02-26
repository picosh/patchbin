package patchbin

import (
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strconv"
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
	ID     int64
	Title  string
	Date   string
	Status Status
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
			FormattedID: getFormattedPatchsetID(patchset.ID),
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

			diffStr, err := parseText(web.Formatter, web.Theme, frag.String())
			if err != nil {
				return nil, err
			}

			hunks = append(hunks, PatchHunk{
				Anchor:   hunkAnchor(patch.ID, fileName, hunkIdx),
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
		if eventlog.PatchsetID.Int64 > 0 {
			logps, err = web.Pr.GetPatchsetByID(eventlog.PatchsetID.Int64)
			if err != nil {
				web.Logger.Error("cannot get patchset", "err", err, "ps", eventlog.PatchsetID)
				return logData, err
			}
			for _, psData := range patchsetsData {
				if psData.ID == eventlog.PatchsetID.Int64 {
					rangeDiff = psData.RangeDiff
					break
				}
			}
		}

		logDisplayName := web.Backend.ComputeUserName(logUser.Pubkey)
		logData = append(logData, EventLogData{
			EventLog:            eventlog,
			FormattedPatchsetID: getFormattedPatchsetID(eventlog.PatchsetID.Int64),
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

func createPrDetail(page string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		prID, err := strconv.Atoi(id)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		web, err := getWebCtx(r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var pr *PatchRequest
		var ps *Patchset
		switch page {
		case "pr":
			{
				pr, err = web.Pr.GetPatchRequestByID(int64(prID))
				if err != nil {
					web.Pr.Backend.Logger.Error("cannot get prs", "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				ps, err = web.Pr.GetLatestPatchsetByPrID(int64(prID))
				if err != nil {
					web.Pr.Backend.Logger.Error("cannot get patchset", "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
		case "ps":
			{
				ps, err = web.Pr.GetPatchsetByID(int64(prID))
				if err != nil {
					web.Pr.Backend.Logger.Error("cannot get patchset", "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				pr, err = web.Pr.GetPatchRequestByID(int64(ps.PatchRequestID))
				if err != nil {
					web.Pr.Backend.Logger.Error("cannot get pr", "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
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
		if patchIDStr := r.PathValue("patchID"); patchIDStr != "" {
			patchID, err := strconv.ParseInt(patchIDStr, 10, 64)
			if err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
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
		}

		selectedPatch, err := getPatchData(web, aps.Patches[selectedIdx].Patch)
		if err != nil {
			web.Logger.Error("cannot compute selected patch data", "err", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		var prevUrl, nextUrl string
		if selectedIdx > 0 {
			prevUrl = fmt.Sprintf("/ps/%d/patches/%d", ps.ID, aps.Patches[selectedIdx-1].ID)
		}
		if selectedIdx < len(aps.Patches)-1 {
			nextUrl = fmt.Sprintf("/ps/%d/patches/%d", ps.ID, aps.Patches[selectedIdx+1].ID)
		}

		logData, err := getLogData(web, pr.ID, aps.Patchsets)
		if err != nil {
			web.Logger.Error("cannot fetch log data", "err", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		w.Header().Set("content-type", "text/html")
		err = prTmpl.Execute(w, PrDetailData{
			Page:                page,
			RepoName:            pr.RepoName,
			Branch:              "main",
			Patchset:            ps,
			FormattedPatchsetID: getFormattedPatchsetID(ps.ID),
			PatchsetDate:        ps.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
			Patches:             aps.Patches,
			Patch:               selectedPatch,
			PrevUrl:             prevUrl,
			NextUrl:             nextUrl,
			Logs:                logData,
			Pr: PrData{
				ID: pr.ID,
				UserData: UserData{
					UserID:    user.ID,
					Name:      displayName,
					IsAdmin:   isAdmin,
					Pubkey:    user.Pubkey,
					CreatedAt: user.CreatedAt.Format(time.RFC3339),
				},
				Title:  pr.Name,
				Date:   pr.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
				Status: pr.Status,
			},
			MetaData: MetaData{
				URL: web.Backend.Cfg.Url,
			},
		})
		if err != nil {
			web.Backend.Logger.Error("cannot execute template", "err", err)
		}
	}
}
