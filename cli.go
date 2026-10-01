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

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
)

func formatTable(sesh io.Writer, render func(w io.Writer)) {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	render(w)
	_ = w.Flush()

	lines := strings.Split(buf.String(), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		if i == 0 {
			_, _ = fmt.Fprintf(sesh, "%s%s%s\n", ansiGray, line, ansiReset)
		} else {
			_, _ = fmt.Fprintln(sesh, line)
		}
	}
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

func prSummary(be *Backend, pr GitPatchRequest, sesh *pssh.SSHServerConnSession, request *PatchRequest) error {
	sesh.Printf("%s● PR %s:%s%s  %s%s%s\n", ansiBold, request.RepoName, request.Slug, ansiReset, ansiBold, request.Name, ansiReset)
	sesh.Printf("  %sRepo:%s   %s\n", ansiGray, ansiReset, request.RepoName)
	sesh.Printf("  %sSlug:%s   %s\n", ansiGray, ansiReset, request.Slug)
	sesh.Printf("  %sURL:%s    https://%s/%s/%s\n", ansiGray, ansiReset, be.Cfg.Url, request.RepoName, request.Slug)
	sesh.Printf("  %sDate:%s   %s\n", ansiGray, ansiReset, request.CreatedAt.Format(be.Cfg.TimeFormat))
	sesh.Printf("\n")

	patchsets, err := pr.GetPatchsetsByPrID(request.ID)
	if err != nil {
		return err
	}

	sesh.Printf("%s▸ Patchsets%s %s(%d total)%s\n", ansiBold, ansiReset, ansiGray, len(patchsets), ansiReset)
	formatTable(sesh, func(w io.Writer) {
		_, _ = fmt.Fprintln(w, "  Rev\tTarget\tUser\tDate")
		for idx, patchset := range patchsets {
			user, err := pr.GetUserByID(patchset.UserID)
			if err != nil {
				be.Logger.Error("cannot find user for patchset", "err", err)
				continue
			}
			displayName := be.ComputeUserName(user.Pubkey)

			_, _ = fmt.Fprintf(
				w,
				"  v%d\t%s:%s.%d\t%s\t%s\n",
				idx+1,
				request.RepoName,
				request.Slug,
				idx+1,
				displayName,
				patchset.CreatedAt.Format(be.Cfg.TimeFormat),
			)
		}
	})

	latest := patchsets[len(patchsets)-1]
	patches, err := pr.GetPatchesByPatchsetID(latest.ID)
	if err != nil {
		return err
	}

	sesh.Printf("\n%s▸ Patches%s %s(latest: v%d)%s\n", ansiBold, ansiReset, ansiGray, len(patchsets), ansiReset)
	formatTable(sesh, func(w io.Writer) {
		_, _ = fmt.Fprintln(w, "  #\tCommit\tAuthor\tDate\tTitle")
		for idx, patch := range patches {
			timestamp := patch.AuthorDate.Format(be.Cfg.TimeFormat)
			author := patch.AuthorName
			if patch.AuthorEmail != "" {
				author = fmt.Sprintf("%s <%s>", patch.AuthorName, patch.AuthorEmail)
			}
			_, _ = fmt.Fprintf(
				w,
				"  %d\t%s\t%s\t%s\t%s\n",
				idx+1,
				truncateSha(patch.CommitSha),
				author,
				timestamp,
				patch.Title,
			)
		}
	})
	return nil
}

// printCoverLetterForPatchset prints patches with a cover letter and discussion.
func printCoverLetterForPatchset(sesh *pssh.SSHServerConnSession, be *Backend, gpr GitPatchRequest, ps *Patchset) error {
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

There's no accept/reject step. A patch request is simply active
or inactive: active ones go inactive after 14 days without activity.
When a reviewer is happy with the code, they pull it, merge it, and
push upstream themselves; there's nothing to manage here beyond that.

QUICKSTART

  Submit a patch request (new or follow-up):
    git format-patch main --stdout | ssh %[2]s {repo}:{slug}

  Pull the latest patchset for checkout (pipes to git am):
    ssh %[2]s pull {repo}:{slug} | git am -3
    ssh %[2]s {repo}:{slug}.patch | git am -3

  View PR metadata and discussion:
    ssh %[2]s show {repo}:{slug}

COMMANDS

  {repo}:{slug}
    Submit a patchset from stdin (creates PR if new, appends if exists).
    git format-patch main --stdout | ssh %[2]s {repo}:{slug}

  pull {repo}:{slug} [rev]
    Print mbox patchset for checkout (pipes to git am).
    ssh %[2]s pull {repo}:{slug} | git am -3
    ssh %[2]s {repo}:{slug}.patch | git am -3

  show {repo}:{slug}
    Show metadata, patchsets, and patches for a PR.
    ssh %[2]s show {repo}:{slug}

  ls [repo] [--active|--inactive|--mine]
    List patch requests.
    ssh %[2]s ls {repo}

  comment {repo}:{slug} [msg]
    Add a comment to a PR (via argument or stdin).
    ssh %[2]s comment {repo}:{slug} "looks good!"
    echo "looks good!" | ssh %[2]s comment {repo}:{slug}

  edit {repo}:{slug} {title}
    Rename a PR (creator only).
    ssh %[2]s edit {repo}:{slug} "new title"

  rm {repo}:{slug} [rev]
    Remove a patchset and its patches (creator only).
    ssh %[2]s rm {repo}:{slug}.2

  issue {repo}:{slug} [title] [body]
    Submit a new issue (text-only patch request).
    ssh %[2]s issue {repo}:{slug} "crash on boot" "repro steps..."

  logs [--pr {repo}:{slug}] [--pubkey]
    List event logs with filters.
    ssh %[2]s logs --pr {repo}:{slug}

GUARDS

  To limit abuse, submissions are capped at %[3]d bytes of stdin, and
  globally rate limited to %[4]d submissions per %[5]s across all users.

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
				Name:      "push",
				Usage:     "Submit a patchset to <repo>:<slug> (creates PR if new, appends if exists)",
				Args:      true,
				ArgsUsage: "<repo>:<slug>",
				Action: func(cCtx *cli.Context) error {
					if !be.Limiter.Allow() {
						return be.Limiter.Error()
					}

					args := cCtx.Args()
					if !args.Present() {
						return fmt.Errorf("must provide target in format <repo>:<slug> (e.g. pico:feat/login)")
					}

					target, err := ParseTarget(args.First())
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
					if len(strings.TrimSpace(string(body))) == 0 {
						return fmt.Errorf("no patch data received on stdin\n\nTo submit a patch:\n  git format-patch main --stdout | ssh %s %s:%s\n\nTo view this PR:\n  ssh %s show %s:%s\n\nTo pull this patchset:\n  ssh %s pull %s:%s | git am -3", url, target.Repo, target.Slug, url, target.Repo, target.Slug, url, target.Repo, target.Slug)
					}

					prq, err := pr.GetPatchRequestByRepoAndSlug(target.Repo, target.Slug)
					if err != nil {
						// Create new PR
						prq, err = pr.SubmitPatchRequest(user.ID, pubkey, target.Repo, target.Slug, bytes.NewReader(body))
						if err != nil {
							return err
						}
						sesh.Printf("%s✔ PR %s:%s created!%s\n\n", ansiGreen, target.Repo, target.Slug, ansiReset)
						return prSummary(be, pr, sesh, prq)
					}

					// Append patchset to existing PR
					patches, err := pr.SubmitPatchset(prq.ID, user.ID, OpNormal, bytes.NewReader(body))
					if err != nil {
						return err
					}

					if len(patches) == 0 {
						sesh.Printf("%sPatches submitted!%s However none were saved, probably because they already exist in the system.\n\n", ansiYellow, ansiReset)
						return nil
					}

					sesh.Printf("%s✔ Submitted new patchset for PR %s:%s!%s\n\n", ansiGreen, target.Repo, target.Slug, ansiReset)
					return prSummary(be, pr, sesh, prq)
				},
			},
			{
				Name:      "pull",
				Usage:     "Print patches in a patchset for git am checkout",
				Args:      true,
				ArgsUsage: "<repo>:<slug>[.rev] or <repo>:<slug> [rev]",
				Action: func(cCtx *cli.Context) error {
					args := cCtx.Args()
					if !args.Present() {
						return fmt.Errorf("must provide target in format <repo>:<slug> (e.g. pico:feat/login)")
					}

					raw := args.First()
					if args.Len() > 1 {
						if rev, err := strconv.Atoi(args.Get(1)); err == nil && rev > 0 {
							raw = fmt.Sprintf("%s.%d", strings.TrimSuffix(raw, ".patch"), rev)
						}
					}

					_, ps, err := ResolveTarget(pr, raw)
					if err != nil {
						return err
					}

					return printCoverLetterForPatchset(sesh, be, pr, ps)
				},
			},
			{
				Name:      "show",
				Usage:     "Show metadata, patchsets, and patches for a PR",
				Args:      true,
				ArgsUsage: "<repo>:<slug>",
				Action: func(cCtx *cli.Context) error {
					args := cCtx.Args()
					if !args.Present() {
						return fmt.Errorf("must provide target in format <repo>:<slug> (e.g. pico:feat/login)")
					}

					target, err := ParseTarget(args.First())
					if err != nil {
						return err
					}

					prq, err := pr.GetPatchRequestByRepoAndSlug(target.Repo, target.Slug)
					if err != nil {
						return fmt.Errorf("cannot find PR %s:%s", target.Repo, target.Slug)
					}

					return prSummary(be, pr, sesh, prq)
				},
			},
			{
				Name:      "ls",
				Usage:     "List patch requests",
				Args:      true,
				ArgsUsage: "[repo]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "active",
						Usage: "only show active PRs (activity in last 14 days)",
					},
					&cli.BoolFlag{
						Name:  "inactive",
						Usage: "only show inactive PRs (no activity in 14 days)",
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
					} else {
						prs, err = pr.GetPatchRequestsByRepoName(repoName)
					}
					if err != nil {
						return err
					}

					onlyActive := cCtx.Bool("active")
					onlyInactive := cCtx.Bool("inactive")
					onlyMine := cCtx.Bool("mine")
					cutoff := time.Now().AddDate(0, 0, -14)

					if repoName == "" {
						sesh.Printf("%s▸ Patch Requests%s\n\n", ansiBold, ansiReset)
					} else {
						sesh.Printf("%s▸ Patch Requests%s %s(%s)%s\n\n", ansiBold, ansiReset, ansiGray, repoName, ansiReset)
					}

					var matching []*PatchRequest
					for _, req := range prs {
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

						matching = append(matching, req)
					}

					if len(matching) == 0 {
						sesh.Printf("  %s(No patch requests found)%s\n", ansiGray, ansiReset)
						return nil
					}

					formatTable(sesh, func(w io.Writer) {
						_, _ = fmt.Fprintln(w, "  Target\tPatchsets\tUser\tLast Activity\tTitle")
						for _, req := range matching {
							user, _ := pr.GetUserByID(req.UserID)
							patchsets, err := pr.GetPatchsetsByPrID(req.ID)
							if err != nil {
								be.Logger.Error("could not get patchsets for pr", "err", err)
								continue
							}

							displayName := be.ComputeUserName(user.Pubkey)

							_, _ = fmt.Fprintf(
								w,
								"  %s:%s\t%d\t%s\t%s\t%s\n",
								req.RepoName,
								req.Slug,
								len(patchsets),
								displayName,
								req.LastActivity.Format(be.Cfg.TimeFormat),
								req.Name,
							)
						}
					})
					return nil
				},
			},
			{
				Name:      "comment",
				Usage:     "Add a comment to a PR",
				Args:      true,
				ArgsUsage: "<repo>:<slug> [message]",
				Action: func(cCtx *cli.Context) error {
					if !be.Limiter.Allow() {
						return be.Limiter.Error()
					}

					args := cCtx.Args()
					if !args.Present() {
						return fmt.Errorf("must provide target in format <repo>:<slug> (e.g. pico:feat/login)")
					}

					target, err := ParseTarget(args.First())
					if err != nil {
						return err
					}

					prq, err := pr.GetPatchRequestByRepoAndSlug(target.Repo, target.Slug)
					if err != nil {
						return fmt.Errorf("cannot find PR %s:%s", target.Repo, target.Slug)
					}

					user, err := pr.UpsertUserByPubkey(pubkey)
					if err != nil {
						return err
					}

					var comment string
					if args.Len() > 1 {
						comment = strings.TrimSpace(strings.Join(args.Slice()[1:], " "))
					} else {
						body, err := readStdinLimited(sesh, be.Cfg.MaxStdinBytes)
						if err != nil {
							return fmt.Errorf("failed to read comment from stdin: %w", err)
						}
						comment = strings.TrimSpace(string(body))
					}

					if comment == "" {
						return fmt.Errorf("must provide comment via argument or stdin")
					}

					err = pr.AddComment(prq.ID, user.ID, comment)
					if err != nil {
						return err
					}

					sesh.Printf("%s✔ Comment added to PR %s:%s!%s\n\n", ansiGreen, prq.RepoName, prq.Slug, ansiReset)
					return nil
				},
			},
			{
				Name:      "edit",
				Usage:     "Edit a PR's title (creator only)",
				Args:      true,
				ArgsUsage: "<repo>:<slug> <title>",
				Action: func(cCtx *cli.Context) error {
					args := cCtx.Args()
					if !args.Present() {
						return fmt.Errorf("must provide target in format <repo>:<slug> (e.g. pico:feat/login)")
					}

					target, err := ParseTarget(args.First())
					if err != nil {
						return err
					}

					prq, err := pr.GetPatchRequestByRepoAndSlug(target.Repo, target.Slug)
					if err != nil {
						return fmt.Errorf("cannot find PR %s:%s", target.Repo, target.Slug)
					}

					if args.Len() < 2 {
						return fmt.Errorf("must provide new title")
					}
					title := strings.TrimSpace(strings.Join(args.Slice()[1:], " "))
					if title == "" {
						return fmt.Errorf("must provide new title")
					}

					err = pr.UpdatePatchRequestName(prq.ID, pubkey, title)
					if err != nil {
						return err
					}

					sesh.Printf("%s✔ Updated PR %s:%s title to: %s%s\n\n", ansiGreen, prq.RepoName, prq.Slug, title, ansiReset)
					return nil
				},
			},
			{
				Name:      "rm",
				Usage:     "Remove a patchset and its patches (creator only)",
				Args:      true,
				ArgsUsage: "<repo>:<slug>.<rev> or <repo>:<slug> [rev]",
				Action: func(cCtx *cli.Context) error {
					args := cCtx.Args()
					if !args.Present() {
						return fmt.Errorf("must provide target in format <repo>:<slug>.<rev> (e.g. pico:feat/login.2)")
					}

					raw := args.First()
					if args.Len() > 1 {
						if rev, err := strconv.Atoi(args.Get(1)); err == nil && rev > 0 {
							raw = fmt.Sprintf("%s.%d", raw, rev)
						}
					}

					prq, patchset, err := ResolveTarget(pr, raw)
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

					rev := getPatchsetRev(pr, patchset)
					err = pr.DeletePatchsetByID(user.ID, prq.ID, patchset.ID)
					if err != nil {
						return err
					}

					sesh.Printf("%s✔ Removed patchset %s:%s.%d.%s\n", ansiGreen, prq.RepoName, prq.Slug, rev, ansiReset)
					return nil
				},
			},
			{
				Name:      "issue",
				Usage:     "Submit a new issue (text-only patch request)",
				Args:      true,
				ArgsUsage: "<repo>:<slug> [title] [body]",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "title",
						Usage: "issue title (default: first line of stdin or 2nd argument)",
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
						return fmt.Errorf("must provide target in format <repo>:<slug> (e.g. pico:issue-1)")
					}

					target, err := ParseTarget(args.First())
					if err != nil {
						return err
					}

					title := cCtx.String("title")
					var bodyStr string

					if args.Len() > 1 {
						if title == "" {
							title = args.Get(1)
							if args.Len() > 2 {
								bodyStr = strings.Join(args.Slice()[2:], " ")
							}
						} else {
							bodyStr = strings.Join(args.Slice()[1:], " ")
						}
					}

					if bodyStr == "" {
						body, err := readStdinLimited(sesh, be.Cfg.MaxStdinBytes)
						if err != nil {
							return fmt.Errorf("failed to read issue body from stdin: %w", err)
						}
						bodyStr = strings.TrimSpace(string(body))
					}

					if title == "" {
						if bodyStr == "" {
							return fmt.Errorf("must provide issue title or body")
						}
						lines := strings.SplitN(bodyStr, "\n", 2)
						title = lines[0]
						if len(lines) > 1 {
							bodyStr = strings.TrimSpace(lines[1])
						} else {
							bodyStr = ""
						}
					}

					prq, err := pr.SubmitIssue(user.ID, pubkey, target.Repo, target.Slug, title, bodyStr)
					if err != nil {
						return err
					}

					sesh.Printf("%s✔ Issue %s:%s created!%s\n\n", ansiGreen, prq.RepoName, prq.Slug, ansiReset)
					return prSummary(be, pr, sesh, prq)
				},
			},
			{
				Name:  "logs",
				Usage: "List event logs with filters",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "pr",
						Usage: "show all events related to the provided PR (<repo>:<slug>)",
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
					prTarget := cCtx.String("pr")
					var eventLogs []*EventLog
					if isPubkey {
						eventLogs, err = pr.GetEventLogsByUserID(user.ID)
					} else if prTarget != "" {
						var prq *PatchRequest
						prq, _, err = ResolveTarget(pr, prTarget)
						if err != nil {
							return err
						}
						eventLogs, err = pr.GetEventLogsByPrID(prq.ID)
					} else {
						eventLogs, err = pr.GetEventLogs()
					}
					if err != nil {
						return err
					}

					sesh.Printf("%s▸ Event Logs%s\n\n", ansiBold, ansiReset)
					if len(eventLogs) == 0 {
						sesh.Printf("  %s(No event logs found)%s\n", ansiGray, ansiReset)
						return nil
					}

					formatTable(sesh, func(w io.Writer) {
						_, _ = fmt.Fprintln(w, "  Target\tPatchset\tEvent\tCreated\tData")
						for _, eventLog := range eventLogs {
							targetStr := "-"
							if eventLog.PatchRequestID.Valid && eventLog.PatchRequestID.Int64 > 0 {
								prq, err := pr.GetPatchRequestByID(eventLog.PatchRequestID.Int64)
								if err == nil {
									targetStr = fmt.Sprintf("%s:%s", prq.RepoName, prq.Slug)
								} else {
									targetStr = fmt.Sprintf("#%d", eventLog.PatchRequestID.Int64)
								}
							}

							psIDStr := "-"
							if eventLog.PatchsetID.Valid && eventLog.PatchsetID.Int64 > 0 {
								ps, err := pr.GetPatchsetByID(eventLog.PatchsetID.Int64)
								if err == nil {
									rev := getPatchsetRev(pr, ps)
									psIDStr = fmt.Sprintf("v%d", rev)
								} else {
									psIDStr = fmt.Sprintf("v%d", eventLog.PatchsetID.Int64)
								}
							}

							_, _ = fmt.Fprintf(
								w,
								"  %s\t%s\t%s\t%s\t%s\n",
								targetStr,
								psIDStr,
								eventLog.Event,
								eventLog.CreatedAt.Format(be.Cfg.TimeFormat),
								eventLog.Data,
							)
						}
					})
					return nil
				},
			},
		},
	}

	return app
}
