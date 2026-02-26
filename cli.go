package patchbin

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/picosh/pico/pkg/pssh"
	"github.com/urfave/cli/v2"
)

func NewTabWriter(out io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(out, 0, 0, 1, ' ', tabwriter.TabIndent)
}

func strToInt(str string) (int64, error) {
	prID, err := strconv.ParseInt(str, 10, 64)
	return prID, err
}

// readStdinLimited reads all of stdin, rejecting input over maxBytes rather
// than silently truncating it.
func readStdinLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(r, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("stdin exceeds max size of %d bytes", maxBytes)
	}
	return body, nil
}

func getPatchsetFromOpt(patchsets []*Patchset, optPatchsetID string) (*Patchset, error) {
	if optPatchsetID == "" {
		return patchsets[len(patchsets)-1], nil
	}

	id, err := getPatchsetID(optPatchsetID)
	if err != nil {
		return nil, err
	}

	for _, ps := range patchsets {
		if ps.ID == id {
			return ps, nil
		}
	}

	return nil, fmt.Errorf("cannot find patchset: %s", optPatchsetID)
}

func prSummary(be *Backend, pr GitPatchRequest, sesh *pssh.SSHServerConnSession, prID int64) error {
	request, err := pr.GetPatchRequestByID(prID)
	if err != nil {
		return err
	}

	sesh.Printf("Info\n====\n")
	sesh.Printf("URL: https://%s/prs/%d\n", be.Cfg.Url, prID)
	sesh.Printf("Repo: %s\n\n", request.RepoName)

	writer := NewTabWriter(sesh)
	_, _ = fmt.Fprintln(writer, "ID\tName\tStatus\tDate")
	_, _ = fmt.Fprintf(
		writer,
		"%d\t%s\t[%s]\t%s\n",
		request.ID, request.Name, request.Status, request.CreatedAt.Format(be.Cfg.TimeFormat),
	)
	_ = writer.Flush()

	patchsets, err := pr.GetPatchsetsByPrID(prID)
	if err != nil {
		return err
	}

	sesh.Printf("\nPatchsets\n====\n")

	writerSet := NewTabWriter(sesh)
	_, _ = fmt.Fprintln(writerSet, "ID\tUser\tDate")
	for _, patchset := range patchsets {
		user, err := pr.GetUserByID(patchset.UserID)
		if err != nil {
			be.Logger.Error("cannot find user for patchset", "err", err)
			continue
		}
		displayName := be.ComputeUserName(user.Pubkey)

		_, _ = fmt.Fprintf(
			writerSet,
			"%s\t%s\t%s\n",
			getFormattedPatchsetID(patchset.ID),
			displayName,
			patchset.CreatedAt.Format(be.Cfg.TimeFormat),
		)
	}
	_ = writerSet.Flush()

	latest, err := getPatchsetFromOpt(patchsets, "")
	if err != nil {
		return err
	}

	patches, err := pr.GetPatchesByPatchsetID(latest.ID)
	if err != nil {
		return err
	}

	sesh.Printf("\nPatches from latest patchset\n====\n")

	opatches := patches
	w := NewTabWriter(sesh)
	_, _ = fmt.Fprintln(w, "Idx\tTitle\tCommit\tAuthor\tDate")
	for idx, patch := range opatches {
		timestamp := patch.AuthorDate.Format(be.Cfg.TimeFormat)
		_, _ = fmt.Fprintf(
			w,
			"%d\t%s\t%s\t%s <%s>\t%s\n",
			idx,
			patch.Title,
			truncateSha(patch.CommitSha),
			patch.AuthorName,
			patch.AuthorEmail,
			timestamp,
		)
	}
	_ = w.Flush()
	return nil
}

// printCoverLetterFromPrID prints patches with a cover letter and discussion.
func printCoverLetterFromPrID(sesh *pssh.SSHServerConnSession, be *Backend, gpr GitPatchRequest, prID int64) error {
	pr, err := gpr.GetPatchRequestByID(prID)
	if err != nil {
		return err
	}

	patchsets, err := gpr.GetPatchsetsByPrID(prID)
	if err != nil {
		return err
	}
	ps := patchsets[len(patchsets)-1]

	patches, err := gpr.GetPatchesByPatchsetID(ps.ID)
	if err != nil {
		return err
	}

	events, err := gpr.GetEventLogsByPrID(prID)
	if err != nil {
		return err
	}

	users := resolveUsers(gpr, events)

	mbox := GenerateMboxWithCoverLetter(pr, patches, events, users, be.Cfg.Url)
	sesh.Println(mbox)
	return nil
}

// printCoverLetterFromPsID prints patches with a cover letter and discussion.
func printCoverLetterFromPsID(sesh *pssh.SSHServerConnSession, be *Backend, gpr GitPatchRequest, psID int64) error {
	ps, err := gpr.GetPatchsetByID(psID)
	if err != nil {
		return err
	}

	pr, err := gpr.GetPatchRequestByID(ps.PatchRequestID)
	if err != nil {
		return err
	}

	patches, err := gpr.GetPatchesByPatchsetID(ps.ID)
	if err != nil {
		return err
	}

	events, err := gpr.GetEventLogsByPrID(ps.PatchRequestID)
	if err != nil {
		return err
	}

	users := resolveUsers(gpr, events)

	mbox := GenerateMboxWithCoverLetter(pr, patches, events, users, be.Cfg.Url)
	sesh.Println(mbox)
	return nil
}

// resolveUsers loads user records for all user IDs referenced in events.
func resolveUsers(gpr GitPatchRequest, events []*EventLog) map[int64]*User {
	users := make(map[int64]*User)
	for _, event := range events {
		if _, ok := users[event.UserID]; !ok {
			user, err := gpr.GetUserByID(event.UserID)
			if err == nil {
				users[event.UserID] = user
			}
		}
	}
	return users
}

func NewCli(sesh *pssh.SSHServerConnSession, be *Backend, pr GitPatchRequest) *cli.App {
	url := be.Cfg.Url
	desc := fmt.Sprintf(`patchbin (v%s): a pastebin for patches, supercharged for git collaboration.

Contributions are anonymous: connect with an SSH key, no signup. A patch
request works like a pull request, except both sides collaborate by
sending rounds of patchsets -- as commits, not comments -- back and forth
on top of each other. Reviewing means pulling the code down, not clicking
through a diff viewer. An issue is just a patch request without any code
attached yet, so anyone can follow up with a real patch request on top of it.

There's no accept/reject step. A PR is either draft (visible only to you)
or open (visible to everyone, appears in RSS). It goes inactive after 30
days without activity; a reviewer who's happy just pulls it, merges it, and
pushes upstream themselves.

COMMANDS

pr - manage patch requests

  pr create {repo}
    Submit a new PR from stdin (starts as draft).
    git format-patch main --stdout | ssh %[2]s pr create {repo}

  pr add {prID}
    Add a new patchset to an existing PR from stdin.
    git format-patch main --stdout | ssh %[2]s pr add {prID}

  pr open {prID} [--comment]
    Transition draft -> open, enables RSS notifications.
    ssh %[2]s pr open {prID}

  pr draft {prID} [--comment]
    Transition open -> draft, disables RSS notifications.
    ssh %[2]s pr draft {prID}

  pr edit {prID} {title}
    Rename a PR.
    ssh %[2]s pr edit {prID} "new title"

  pr summary {prID}
    Show metadata, patchsets, and patches for a PR.
    ssh %[2]s pr summary {prID}

  pr ls [repo] [--draft|--open|--active|--inactive|--mine]
    List PRs.
    ssh %[2]s pr ls {repo} --open

issue - text-only patch requests (no code required)

  issue create {repo} [--title]
    Submit a new issue from stdin (starts as open).
    echo "steps to reproduce..." | ssh %[2]s issue create {repo} --title "bug: crash on startup"

ps - manage patchsets

  ps rm {patchsetID}
    Remove a patchset and its patches (creator only).
    ssh %[2]s ps rm ps-{patchsetID}

print - print patches for checkout

  print pr-{prID}
    Print the latest patchset for a PR.
    ssh %[2]s print pr-{prID} | git am -3

  print ps-{patchsetID}
    Print a specific patchset.
    ssh %[2]s print ps-{patchsetID} | git am -3

  Cover letters are stored as an empty commit. If you want to keep them
  when applying, use "git am --keep-empty" (or set it globally with
  "git config --global am.keepEmpty true").

logs - event history

  logs [--pr ID] [--pubkey]
    List event logs, optionally filtered to a PR or your own activity.
    ssh %[2]s logs --pr {prID}

STDIN

  pr create, pr add        expect the output of "git format-patch --stdout"
  issue create              expects free-form text (the issue body)
  pr open/draft --comment  expects free-form text (a comment to attach to the status change)

GUARDS

  To limit abuse, submissions (pr create, pr add, issue create) are capped
  at %[3]d bytes of stdin, and globally rate limited to %[4]d submissions
  per %[5]s across all users. Contact an admin if you hit these limits.

  Admins with shell access to the host can ban a pubkey or IP address by
  inserting a row directly into the "acl" table of the sqlite database:

    sqlite3 data/pr.db "INSERT INTO acl (pubkey, permission) VALUES ('{pubkey}', 'banned')"
    sqlite3 data/pr.db "INSERT INTO acl (ip_address, permission) VALUES ('{ip}', 'banned')"

  Banned pubkeys/IPs are rejected at SSH auth time. There is currently no
  SSH command for this; it requires direct database access.

Self-host your own patchbin: https://github.com/picosh/patchbin
`, GITPR_VERSION, url, be.Cfg.MaxStdinBytes, be.Cfg.RateLimitCount, be.Cfg.RateLimitInterval)

	pubkey := be.Pubkey(sesh.PublicKey())
	app := &cli.App{
		Name:                  "ssh",
		Description:           desc,
		Usage:                 "A pastebin for patches, supercharged for git collaboration",
		CustomAppHelpTemplate: "{{.Description}}\n",
		Writer:                sesh,
		ErrWriter:             sesh,
		ExitErrHandler: func(cCtx *cli.Context, err error) {
			if err != nil {
				sesh.Fatal(fmt.Errorf("err: %w", err))
			}
		},
		OnUsageError: func(cCtx *cli.Context, err error, isSubcommand bool) error {
			if err != nil {
				sesh.Fatal(fmt.Errorf("err: %w", err))
			}
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "issue",
				Usage: "Manage issues (text-only patch requests)",
				Subcommands: []*cli.Command{
					{
						Name:      "create",
						Usage:     "Submit a new issue (starts as open)",
						Args:      true,
						ArgsUsage: "repoName",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "title",
								Usage: "issue title (default: first line of stdin)",
							},
						},
						Action: func(cCtx *cli.Context) error {
							if !be.Limiter.Allow() {
								return be.Limiter.Error()
							}

							user, err := pr.UpsertUserByPubkey(pubkey)
							if err != nil {
								return err
							}

							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a repo name")
							}
							repoName := args.First()

							body, err := readStdinLimited(sesh, be.Cfg.MaxStdinBytes)
							if err != nil {
								return fmt.Errorf("failed to read issue body from stdin: %w", err)
							}
							bodyStr := strings.TrimSpace(string(body))
							if bodyStr == "" {
								return fmt.Errorf("must provide issue body via stdin")
							}

							title := cCtx.String("title")
							if title == "" {
								// Use first line as title
								lines := strings.SplitN(bodyStr, "\n", 2)
								title = lines[0]
								if len(lines) > 1 {
									bodyStr = strings.TrimSpace(lines[1])
								} else {
									bodyStr = ""
								}
							}

							prq, err := pr.SubmitIssue(user.ID, pubkey, repoName, title, bodyStr)
							if err != nil {
								return err
							}

							sesh.Printf("Issue created! #%d\n", prq.ID)
							return prSummary(be, pr, sesh, prq.ID)
						},
					},
				},
			},
			{
				Name:  "logs",
				Usage: "List event logs with filters",
				Args:  true,
				Flags: []cli.Flag{
					&cli.Int64Flag{
						Name:  "pr",
						Usage: "show all events related to the provided patch request",
					},
					&cli.BoolFlag{
						Name:  "pubkey",
						Usage: "show all events related to your pubkey",
					},
				},
				Action: func(cCtx *cli.Context) error {
					user, err := pr.UpsertUserByPubkey(pubkey)
					if err != nil {
						return err
					}
					isPubkey := cCtx.Bool("pubkey")
					prID := cCtx.Int64("pr")
					var eventLogs []*EventLog
					if isPubkey {
						eventLogs, err = pr.GetEventLogsByUserID(user.ID)
					} else if prID != 0 {
						eventLogs, err = pr.GetEventLogsByPrID(prID)
					} else {
						eventLogs, err = pr.GetEventLogs()
					}
					if err != nil {
						return err
					}

					writer := NewTabWriter(sesh)
					_, _ = fmt.Fprintln(writer, "PrID\tPatchsetID\tEvent\tCreated\tData")
					for _, eventLog := range eventLogs {
						_, _ = fmt.Fprintf(
							writer,
							"%d\t%s\t%s\t%s\t%s\n",
							eventLog.PatchRequestID.Int64,
							getFormattedPatchsetID(eventLog.PatchsetID.Int64),
							eventLog.Event,
							eventLog.CreatedAt.Format(be.Cfg.TimeFormat),
							eventLog.Data,
						)
					}
					_ = writer.Flush()
					return nil
				},
			},
			{
				Name:  "ps",
				Usage: "Manage patchsets",
				Subcommands: []*cli.Command{
					{
						Name:      "rm",
						Usage:     "Remove a patchset and its patches",
						Args:      true,
						ArgsUsage: "[patchsetID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patchset ID")
							}

							patchsetID, err := getPatchsetID(args.First())
							if err != nil {
								return err
							}

							patchset, err := pr.GetPatchsetByID(patchsetID)
							if err != nil {
								return err
							}

							user, err := pr.GetUserByID(patchset.UserID)
							if err != nil {
								return err
							}

							if pubkey != user.Pubkey {
								return fmt.Errorf("you are not authorized to delete this patchset (only the creator can delete)")
							}

							err = pr.DeletePatchsetByID(user.ID, patchset.PatchRequestID, patchsetID)
							if err != nil {
								return err
							}
							sesh.Printf("successfully removed patchset: %d\n", patchsetID)
							return nil
						},
					},
				},
			},
			{
				Name:      "print",
				Usage:     "Print patches in a patchset",
				Args:      true,
				ArgsUsage: "[pr-X] or [ps-X]",
				Action: func(cCtx *cli.Context) error {
					args := cCtx.Args()
					raw := args.First()
					split := strings.Split(raw, "-")
					if len(split) < 2 {
						return fmt.Errorf("must provide ID in format: pr-X, ps-X")
					}

					prefix := split[0]
					id, err := strToInt(split[1])
					if err != nil {
						return err
					}

					switch prefix {
					case "pr":
						err = printCoverLetterFromPrID(sesh, be, pr, id)
					case "ps":
						err = printCoverLetterFromPsID(sesh, be, pr, id)
					default:
						return fmt.Errorf("unknown prefix %q, must be one of: pr, ps", prefix)
					}

					return err
				},
			},
			{
				Name:  "pr",
				Usage: "Manage patch requests (PR)",
				Subcommands: []*cli.Command{
					{
						Name:      "ls",
						Usage:     "List all PRs",
						Args:      true,
						ArgsUsage: "[repoName]",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "draft",
								Usage: "only show draft PRs",
							},
							&cli.BoolFlag{
								Name:  "open",
								Usage: "only show open PRs",
							},
							&cli.BoolFlag{
								Name:  "active",
								Usage: "only show active PRs (activity in last 30 days)",
							},
							&cli.BoolFlag{
								Name:  "inactive",
								Usage: "only show inactive PRs (no activity in 30 days)",
							},
							&cli.BoolFlag{
								Name:  "mine",
								Usage: "only show your own PRs",
							},
						},
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							repoName := args.First()
							var prs []*PatchRequest
							var err error
							if repoName == "" {
								prs, err = pr.GetPatchRequests()
								if err != nil {
									return err
								}
							} else {
								prs, err = pr.GetPatchRequestsByRepoName(repoName)
								if err != nil {
									return err
								}
							}

							onlyDraft := cCtx.Bool("draft")
							onlyOpen := cCtx.Bool("open")
							onlyActive := cCtx.Bool("active")
							onlyInactive := cCtx.Bool("inactive")
							onlyMine := cCtx.Bool("mine")
							cutoff := time.Now().AddDate(0, 0, -30)

							writer := NewTabWriter(sesh)
							_, _ = fmt.Fprintln(writer, "ID\tRepo\tName\tStatus\tPatchsets\tUser\tLast Activity")
							for _, req := range prs {
								if onlyDraft && req.Status != StatusDraft {
									continue
								}

								if onlyOpen && req.Status != StatusOpen {
									continue
								}

								if onlyActive && req.LastActivity.Before(cutoff) {
									continue
								}

								if onlyInactive && req.LastActivity.After(cutoff) {
									continue
								}

								user, err := pr.GetUserByID(req.UserID)
								if err != nil {
									be.Logger.Error("could not get user for pr", "err", err)
									continue
								}

								if onlyMine && user.Pubkey != pubkey {
									continue
								}

								patchsets, err := pr.GetPatchsetsByPrID(req.ID)
								if err != nil {
									be.Logger.Error("could not get patchsets for pr", "err", err)
									continue
								}

								displayName := be.ComputeUserName(user.Pubkey)

								_, _ = fmt.Fprintf(
									writer,
									"%d\t%s\t%s\t[%s]\t%d\t%s\t%s\n",
									req.ID,
									req.RepoName,
									req.Name,
									req.Status,
									len(patchsets),
									displayName,
									req.LastActivity.Format(be.Cfg.TimeFormat),
								)
							}
							_ = writer.Flush()
							return nil
						},
					},
					{
						Name:      "create",
						Usage:     "Submit a new PR (starts as draft)",
						Args:      true,
						ArgsUsage: "repoName",
						Action: func(cCtx *cli.Context) error {
							if !be.Limiter.Allow() {
								return be.Limiter.Error()
							}

							user, err := pr.UpsertUserByPubkey(pubkey)
							if err != nil {
								return err
							}

							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a repo name")
							}
							repoName := args.First()

							body, err := readStdinLimited(sesh, be.Cfg.MaxStdinBytes)
							if err != nil {
								return fmt.Errorf("failed to read patchset from stdin: %w", err)
							}

							prq, err := pr.SubmitPatchRequest(user.ID, pubkey, repoName, bytes.NewReader(body))
							if err != nil {
								return err
							}
							sesh.Println(
								"PR submitted as draft! Use `pr open <id>` to make it visible.",
							)

							return prSummary(be, pr, sesh, prq.ID)
						},
					},
					{
						Name:      "open",
						Usage:     "Transition PR to open (enable RSS notifications)",
						Args:      true,
						ArgsUsage: "[prID]",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "comment",
								Usage: "If this flag is provided, pass comment through stdin",
							},
						},
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}

							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							if prq.Status == StatusOpen {
								return fmt.Errorf("PR is already open")
							}

							comment := cCtx.Bool("comment")
							var commentTxt []byte
							if comment {
								commentTxt, err = io.ReadAll(sesh)
								if err != nil {
									return fmt.Errorf("when comment flag enabled must provide it from stdin")
								}
							}

							err = pr.UpdatePatchRequestStatus(prID, pubkey, StatusOpen, string(commentTxt))
							if err != nil {
								return err
							}
							sesh.Printf("Opened PR %s (#%d)\n", prq.Name, prq.ID)
							return prSummary(be, pr, sesh, prID)
						},
					},
					{
						Name:      "draft",
						Usage:     "Transition PR to draft (disable RSS notifications)",
						Args:      true,
						ArgsUsage: "[prID]",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "comment",
								Usage: "If this flag is provided, pass comment through stdin",
							},
						},
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}

							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							if prq.Status == StatusDraft {
								return fmt.Errorf("PR is already a draft")
							}

							comment := cCtx.Bool("comment")
							var commentTxt []byte
							if comment {
								commentTxt, err = io.ReadAll(sesh)
								if err != nil {
									return fmt.Errorf("when comment flag enabled must provide it from stdin")
								}
							}

							err = pr.UpdatePatchRequestStatus(prID, pubkey, StatusDraft, string(commentTxt))
							if err != nil {
								return err
							}
							sesh.Printf("Drafted PR %s (#%d)\n", prq.Name, prq.ID)
							return prSummary(be, pr, sesh, prID)
						},
					},
					{
						Name:      "summary",
						Usage:     "Show metadata, patchsets, and patches for a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}
							return prSummary(be, pr, sesh, prID)
						},
					},
					{
						Name:      "edit",
						Usage:     "Edit a PR's title",
						Args:      true,
						ArgsUsage: "[prID] [title]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}
							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							tail := cCtx.Args().Tail()
							title := strings.Join(tail, " ")
							if title == "" {
								return fmt.Errorf("must provide title")
							}

							err = pr.UpdatePatchRequestName(prID, pubkey, title)
							if err != nil {
								return err
							}
							sesh.Printf("New title: %s (%d)\n", title, prq.ID)

							return err
						},
					},
					{
						Name:      "add",
						Usage:     "Add a new patchset to a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Action: func(cCtx *cli.Context) error {
							if !be.Limiter.Allow() {
								return be.Limiter.Error()
							}

							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}
							_, err = pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							user, err := pr.UpsertUserByPubkey(pubkey)
							if err != nil {
								return err
							}

							body, err := readStdinLimited(sesh, be.Cfg.MaxStdinBytes)
							if err != nil {
								return fmt.Errorf("failed to read patchset from stdin: %w", err)
							}

							patches, err := pr.SubmitPatchset(prID, user.ID, OpNormal, bytes.NewReader(body))
							if err != nil {
								return err
							}

							if len(patches) == 0 {
								sesh.Println("Patches submitted! However none were saved, probably because they already exist in the system")
								return nil
							}

							sesh.Println("Patches submitted!")
							return prSummary(be, pr, sesh, prID)
						},
					},
				},
			},
		},
	}

	return app
}
