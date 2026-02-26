package patchbin

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrPatchExists = errors.New("patch already exists for patch request")

type PatchsetOp int

const (
	OpNormal PatchsetOp = iota
)

var ErrNotPrOwner = fmt.Errorf("only the PR creator can perform this action")

type GitPatchRequest interface {
	GetUsers() ([]*User, error)
	GetUserByID(userID int64) (*User, error)
	GetUserByPubkey(pubkey string) (*User, error)
	UpsertUserByPubkey(pubkey string) (*User, error)
	IsBanned(pubkey, ipAddress string) error
	SubmitPatchRequest(userID int64, userPubkey string, repoName string, patchset io.Reader) (*PatchRequest, error)
	SubmitPatchset(prID, userID int64, op PatchsetOp, patchset io.Reader) ([]*Patch, error)
	GetPatchRequestByID(prID int64) (*PatchRequest, error)
	GetPatchRequests() ([]*PatchRequest, error)
	GetPatchRequestsByRepoName(repoName string) ([]*PatchRequest, error)
	GetPatchRequestsByPubkey(pubkey string) ([]*PatchRequest, error)
	GetPatchsetsByPrID(prID int64) ([]*Patchset, error)
	GetPatchsetByID(patchsetID int64) (*Patchset, error)
	GetLatestPatchsetByPrID(prID int64) (*Patchset, error)
	GetPatchesByPatchsetID(patchsetID int64) ([]*Patch, error)
	UpdatePatchRequestStatus(prID int64, userPubkey string, status Status, comment string) error
	UpdatePatchRequestName(prID int64, userPubkey string, name string) error
	DeletePatchsetByID(userID, prID int64, patchsetID int64) error
	SubmitIssue(userID int64, userPubkey string, repoName, title, body string) (*PatchRequest, error)
	CreateEventLog(tx *sqlx.Tx, eventLog EventLog) error
	GetEventLogs() ([]*EventLog, error)
	GetEventLogsByPrID(prID int64) ([]*EventLog, error)
	GetEventLogsByUserID(userID int64) ([]*EventLog, error)
	DiffPatchsets(aset *Patchset, bset *Patchset) ([]*RangeDiffOutput, error)
}

type PrCmd struct {
	Backend *Backend
}

var (
	_ GitPatchRequest = PrCmd{}
	_ GitPatchRequest = (*PrCmd)(nil)
)

func (pr PrCmd) IsBanned(pubkey, ipAddress string) error {
	acl := []*Acl{}
	err := pr.Backend.DB.Select(
		&acl,
		"SELECT * FROM acl WHERE permission='banned' AND (pubkey=? OR ip_address=?)",
		pubkey,
		ipAddress,
	)
	if len(acl) > 0 {
		return fmt.Errorf("user has been banned")
	}
	return err
}

func (pr PrCmd) GetUsers() ([]*User, error) {
	users := []*User{}
	err := pr.Backend.DB.Select(&users, "SELECT * FROM app_users")
	return users, err
}

func (pr PrCmd) GetUserByID(id int64) (*User, error) {
	var user User
	err := pr.Backend.DB.Get(&user, "SELECT * FROM app_users WHERE id=?", id)
	return &user, err
}

func (pr PrCmd) GetUserByPubkey(pubkey string) (*User, error) {
	var user User
	err := pr.Backend.DB.Get(&user, "SELECT * FROM app_users WHERE pubkey=?", pubkey)
	return &user, err
}

func (pr PrCmd) UpsertUserByPubkey(pubkey string) (*User, error) {
	user, err := pr.GetUserByPubkey(pubkey)
	if err == nil {
		return user, nil
	}
	return pr.createUser(pubkey)
}

func (pr PrCmd) createUser(pubkey string) (*User, error) {
	if pubkey == "" {
		return nil, fmt.Errorf("must provide pubkey when creating user")
	}

	var userID int64
	row := pr.Backend.DB.QueryRow(
		"INSERT INTO app_users (pubkey, name) VALUES (?, ?) RETURNING id",
		pubkey,
		pubkey, // Use pubkey as name placeholder (will be computed on read)
	)
	err := row.Scan(&userID)
	if err != nil {
		return nil, err
	}
	if userID == 0 {
		return nil, fmt.Errorf("could not create user")
	}

	user, err := pr.GetUserByID(userID)
	return user, err
}

func (pr PrCmd) GetPatchsetsByPrID(prID int64) ([]*Patchset, error) {
	patchsets := []*Patchset{}
	err := pr.Backend.DB.Select(
		&patchsets,
		"SELECT * FROM patchsets WHERE patch_request_id=? ORDER BY created_at ASC",
		prID,
	)
	if err != nil {
		return patchsets, err
	}
	if len(patchsets) == 0 {
		return patchsets, fmt.Errorf("no patchsets found for patch request: %d", prID)
	}
	return patchsets, nil
}

func (pr PrCmd) GetPatchsetByID(patchsetID int64) (*Patchset, error) {
	var patchset Patchset
	err := pr.Backend.DB.Get(
		&patchset,
		"SELECT * FROM patchsets WHERE id=?",
		patchsetID,
	)
	return &patchset, err
}

func (pr PrCmd) GetLatestPatchsetByPrID(prID int64) (*Patchset, error) {
	patchsets, err := pr.GetPatchsetsByPrID(prID)
	if err != nil {
		return nil, err
	}
	if len(patchsets) == 0 {
		return nil, fmt.Errorf("no patchsets found for patch request: %d", prID)
	}
	return patchsets[len(patchsets)-1], nil
}

func (pr PrCmd) GetPatchesByPatchsetID(patchsetID int64) ([]*Patch, error) {
	patches := []*Patch{}
	err := pr.Backend.DB.Select(
		&patches,
		"SELECT * FROM patches WHERE patchset_id=? ORDER BY created_at ASC, id ASC",
		patchsetID,
	)
	return patches, err
}

func (cmd PrCmd) GetPatchRequests() ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests ORDER BY id DESC",
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestsByStatus(status Status) ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests WHERE status=? ORDER BY last_activity DESC",
		status,
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestsActive() ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests WHERE status='open' AND last_activity >= datetime('now', '-14 days') ORDER BY last_activity DESC",
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestsInactive() ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests WHERE status='open' AND last_activity < datetime('now', '-14 days') ORDER BY last_activity DESC",
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestsByRepoName(repoName string) ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests WHERE repo_name=? ORDER BY id DESC",
		repoName,
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestsByPubkey(pubkey string) ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT pr.* FROM patch_requests pr, app_users au WHERE pr.user_id=au.id AND au.pubkey=? ORDER BY id DESC",
		pubkey,
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestByID(prID int64) (*PatchRequest, error) {
	pr := PatchRequest{}
	err := cmd.Backend.DB.Get(
		&pr,
		"SELECT * FROM patch_requests WHERE id=? ORDER BY created_at DESC",
		prID,
	)
	return &pr, err
}

func (cmd PrCmd) updateLastActivity(prID int64) error {
	_, err := cmd.Backend.DB.Exec(
		"UPDATE patch_requests SET last_activity=? WHERE id=?",
		time.Now(),
		prID,
	)
	return err
}

// UpdatePatchRequestStatus changes the PR status. Only the PR creator (by pubkey) can do this.
func (cmd PrCmd) UpdatePatchRequestStatus(prID int64, userPubkey string, status Status, comment string) error {
	pr, err := cmd.GetPatchRequestByID(prID)
	if err != nil {
		return err
	}

	// Verify the requester is the PR creator
	owner, err := cmd.GetUserByID(pr.UserID)
	if err != nil {
		return err
	}
	if owner.Pubkey != userPubkey {
		return ErrNotPrOwner
	}

	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.Exec(
		"UPDATE patch_requests SET status=? WHERE id=?",
		status,
		prID,
	)
	if err != nil {
		return err
	}

	err = cmd.CreateEventLog(tx, EventLog{
		UserID:         pr.UserID,
		PatchRequestID: sql.NullInt64{Int64: prID, Valid: true},
		Event:          "pr_status_changed",
		Data: EventData{
			Status:  status,
			Comment: comment,
		},
	})
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return cmd.updateLastActivity(prID)
}

// UpdatePatchRequestName changes the PR title. Only the PR creator (by pubkey) can do this.
func (cmd PrCmd) UpdatePatchRequestName(prID int64, userPubkey string, name string) error {
	if name == "" {
		return fmt.Errorf("must provide name in order to update patch request")
	}

	pr, err := cmd.GetPatchRequestByID(prID)
	if err != nil {
		return err
	}

	// Verify the requester is the PR creator
	owner, err := cmd.GetUserByID(pr.UserID)
	if err != nil {
		return err
	}
	if owner.Pubkey != userPubkey {
		return ErrNotPrOwner
	}

	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.Exec(
		"UPDATE patch_requests SET name=? WHERE id=?",
		name,
		prID,
	)
	if err != nil {
		return err
	}

	err = cmd.CreateEventLog(tx, EventLog{
		UserID:         pr.UserID,
		PatchRequestID: sql.NullInt64{Int64: prID, Valid: true},
		Event:          "pr_name_changed",
		Data: EventData{
			Name: name,
		},
	})
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return cmd.updateLastActivity(prID)
}

func (cmd PrCmd) CreateEventLog(tx *sqlx.Tx, eventLog EventLog) error {
	_, err := tx.Exec(
		"INSERT INTO event_logs (user_id, patch_request_id, patchset_id, event, data) VALUES (?, ?, ?, ?, ?)",
		eventLog.UserID,
		eventLog.PatchRequestID.Int64,
		eventLog.PatchsetID.Int64,
		eventLog.Event,
		eventLog.Data,
	)
	if err != nil {
		cmd.Backend.Logger.Error(
			"could not create eventLog",
			"err", err,
		)
	}
	return err
}

func (cmd PrCmd) createPatch(tx *sqlx.Tx, patch *Patch) (int64, error) {
	patchExists := []Patch{}
	_ = cmd.Backend.DB.Select(&patchExists, "SELECT * FROM patches WHERE patchset_id=? AND content_sha=?", patch.PatchsetID, patch.ContentSha)
	if len(patchExists) > 0 {
		return 0, ErrPatchExists
	}

	var patchID int64
	row := tx.QueryRow(
		"INSERT INTO patches (user_id, patchset_id, author_name, author_email, author_date, title, body, body_appendix, commit_sha, content_sha, base_commit_sha, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id",
		patch.UserID,
		patch.PatchsetID,
		patch.AuthorName,
		patch.AuthorEmail,
		patch.AuthorDate,
		patch.Title,
		patch.Body,
		patch.BodyAppendix,
		patch.CommitSha,
		patch.ContentSha,
		patch.BaseCommitSha,
		patch.RawText,
	)
	err := row.Scan(&patchID)
	if err != nil {
		return 0, err
	}
	if patchID == 0 {
		return 0, fmt.Errorf("could not create patch")
	}
	return patchID, err
}

// SubmitPatchRequest creates a new patch request with draft status.
func (cmd PrCmd) SubmitPatchRequest(userID int64, userPubkey string, repoName string, patchset io.Reader) (*PatchRequest, error) {
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	patches, err := ParsePatchset(patchset)
	if err != nil {
		return nil, err
	}

	if len(patches) == 0 {
		return nil, fmt.Errorf("after parsing patchset we didn't find any patches, did you send us an empty patchset?")
	}

	prName := ""
	prText := ""
	if len(patches) > 0 {
		prName = patches[0].Title
		prText = patches[0].Body
	}

	now := time.Now()
	var prID int64
	row := tx.QueryRow(
		"INSERT INTO patch_requests (user_id, repo_name, name, text, status, updated_at, last_activity) VALUES(?, ?, ?, ?, ?, ?, ?) RETURNING id",
		userID,
		repoName,
		prName,
		prText,
		StatusDraft,
		now,
		now,
	)
	err = row.Scan(&prID)
	if err != nil {
		return nil, err
	}
	if prID == 0 {
		return nil, fmt.Errorf("could not create patch request")
	}

	var patchsetID int64
	row = tx.QueryRow(
		"INSERT INTO patchsets (user_id, patch_request_id) VALUES(?, ?) RETURNING id",
		userID,
		prID,
	)
	err = row.Scan(&patchsetID)
	if err != nil {
		return nil, err
	}
	if patchsetID == 0 {
		return nil, fmt.Errorf("could not create patchset")
	}

	for _, patch := range patches {
		patch.UserID = userID
		patch.PatchsetID = patchsetID
		_, err = cmd.createPatch(tx, patch)
		if err != nil {
			return nil, err
		}
	}

	err = cmd.CreateEventLog(tx, EventLog{
		UserID:         userID,
		PatchRequestID: sql.NullInt64{Int64: prID, Valid: true},
		PatchsetID:     sql.NullInt64{Int64: patchsetID, Valid: true},
		Event:          "pr_created",
	})
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}

// SubmitIssue creates a new patch request as an issue (text-only, no patches, starts open).
// The title is the issue subject, body is the full description.
func (cmd PrCmd) SubmitIssue(userID int64, userPubkey string, repoName, title, body string) (*PatchRequest, error) {
	if title == "" {
		return nil, fmt.Errorf("must provide a title for the issue")
	}

	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now()
	var prID int64
	row := tx.QueryRow(
		"INSERT INTO patch_requests (user_id, repo_name, name, text, status, updated_at, last_activity) VALUES(?, ?, ?, ?, ?, ?, ?) RETURNING id",
		userID,
		repoName,
		title,
		body,
		StatusOpen,
		now,
		now,
	)
	err = row.Scan(&prID)
	if err != nil {
		return nil, err
	}
	if prID == 0 {
		return nil, fmt.Errorf("could not create issue")
	}

	// Create an empty initial patchset so the PR has a patchset for the timeline.
	// Patches can be added later with `pr add`.
	var patchsetID int64
	row = tx.QueryRow(
		"INSERT INTO patchsets (user_id, patch_request_id) VALUES(?, ?) RETURNING id",
		userID,
		prID,
	)
	err = row.Scan(&patchsetID)
	if err != nil {
		return nil, err
	}
	if patchsetID == 0 {
		return nil, fmt.Errorf("could not create patchset")
	}

	err = cmd.CreateEventLog(tx, EventLog{
		UserID:         userID,
		PatchRequestID: sql.NullInt64{Int64: prID, Valid: true},
		PatchsetID:     sql.NullInt64{Int64: patchsetID, Valid: true},
		Event:          "pr_created",
	})
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}

func (cmd PrCmd) SubmitPatchset(prID int64, userID int64, op PatchsetOp, patchset io.Reader) ([]*Patch, error) {
	fin := []*Patch{}
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return fin, err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	patches, err := ParsePatchset(patchset)
	if err != nil {
		return fin, err
	}

	var patchsetID int64
	row := tx.QueryRow(
		"INSERT INTO patchsets (user_id, patch_request_id) VALUES(?, ?) RETURNING id",
		userID,
		prID,
	)
	err = row.Scan(&patchsetID)
	if err != nil {
		return nil, err
	}
	if patchsetID == 0 {
		return nil, fmt.Errorf("could not create patchset")
	}

	for _, patch := range patches {
		patch.UserID = userID
		patch.PatchsetID = patchsetID
		patchID, err := cmd.createPatch(tx, patch)
		if err == nil {
			patch.ID = patchID
			fin = append(fin, patch)
		} else {
			if !errors.Is(ErrPatchExists, err) {
				return fin, err
			}
		}
	}

	if len(fin) > 0 {
		err = cmd.CreateEventLog(tx, EventLog{
			UserID:         userID,
			PatchRequestID: sql.NullInt64{Int64: prID, Valid: true},
			PatchsetID:     sql.NullInt64{Int64: patchsetID, Valid: true},
			Event:          "pr_patchset_added",
		})
		if err != nil {
			return fin, err
		}
	}

	err = tx.Commit()
	if err != nil {
		return fin, err
	}

	// Update last_activity
	if err := cmd.updateLastActivity(prID); err != nil {
		cmd.Backend.Logger.Error("failed to update last_activity", "err", err, "prID", prID)
	}

	return fin, nil
}

func (cmd PrCmd) DeletePatchsetByID(userID int64, prID int64, patchsetID int64) error {
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.Exec(
		"DELETE FROM patchsets WHERE id=?",
		patchsetID,
	)
	if err != nil {
		return err
	}

	err = cmd.CreateEventLog(tx, EventLog{
		UserID:         userID,
		PatchRequestID: sql.NullInt64{Int64: prID, Valid: true},
		PatchsetID:     sql.NullInt64{Int64: patchsetID, Valid: true},
		Event:          "pr_patchset_deleted",
	})
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return cmd.updateLastActivity(prID)
}

func (cmd PrCmd) GetEventLogs() ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	err := cmd.Backend.DB.Select(
		&eventLogs,
		"SELECT * FROM event_logs ORDER BY created_at DESC",
	)
	return eventLogs, err
}

func (cmd PrCmd) GetEventLogsByPrID(prID int64) ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	err := cmd.Backend.DB.Select(
		&eventLogs,
		"SELECT * FROM event_logs WHERE patch_request_id=? ORDER BY created_at DESC",
		prID,
	)
	return eventLogs, err
}

func (cmd PrCmd) GetEventLogsByUserID(userID int64) ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	query := `SELECT * FROM event_logs
	WHERE user_id=?
		OR patch_request_id IN (
			SELECT id FROM patch_requests WHERE user_id=?
		)
	ORDER BY created_at DESC`
	err := cmd.Backend.DB.Select(
		&eventLogs,
		query,
		userID,
		userID,
	)
	return eventLogs, err
}

func (cmd PrCmd) DiffPatchsets(prev *Patchset, next *Patchset) ([]*RangeDiffOutput, error) {
	output := []*RangeDiffOutput{}
	patches, err := cmd.GetPatchesByPatchsetID(next.ID)
	if err != nil {
		return output, err
	}

	for idx, patch := range patches {
		patchStr := patch.RawText
		if idx > 0 {
			patchStr = startOfPatch + patch.RawText
		}
		diffFiles, _, err := ParsePatch(patchStr)
		if err != nil {
			continue
		}
		patch.Files = diffFiles
	}

	if prev == nil {
		return output, nil
	}

	prevPatches, err := cmd.GetPatchesByPatchsetID(prev.ID)
	if err != nil {
		return output, fmt.Errorf("cannot get previous patchset patches: %w", err)
	}

	for idx, patch := range prevPatches {
		patchStr := patch.RawText
		if idx > 0 {
			patchStr = startOfPatch + patch.RawText
		}
		diffFiles, _, err := ParsePatch(patchStr)
		if err != nil {
			continue
		}
		patch.Files = diffFiles
	}

	return RangeDiff(prevPatches, patches), nil
}
