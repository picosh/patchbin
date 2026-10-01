# Changelog

Use spec: https://common-changelog.org/

## v2026-10-01

### Added

- `pr comment {prID}` command to add comments to a PR from stdin (renders in web timeline, bumps PR activity, and appears in cover letter discussion)

### Changed

- PRs are created using a new format: `ssh pr {repo}:{pr_name}`
- Changed the names of many cli commands, run `ssh pr help` to learn more

## v2026-08-31

### Changed

- Removed the concept of a `draft` patch request

## v2026-08-18

### Added

- Zig semantic summary

### Changed

- Better syntax highlighting for line-diffs

## v2026-08-09

### Added

- Issues API: a thin wrapper around patch requests for actionable text-only submissions
  - `echo "does this work?" | ssh {host} issue create --repo xxx`
  - Creates a PR in the "open" state with an empty-commit cover letter
- Cover letter now preserves full PR history (revisions, comments) in the commit message body, with a link back to the PR
- Semantic summary view: shows changed/added/removed functions and methods for at-a-glance review instead of a line diff (via tree-sitter; supports Go, Rust, Python, TS, JS)
- Abuse guards on submissions (`pr create`, `pr add`, `issue create`): a global rate limit (configurable via `rate_limit_count`/`rate_limit_interval` in the toml) and a max stdin size (configurable via `max_stdin_bytes`); both are documented in the SSH CLI help text

### Changed

- *BREAKING*: Renamed the project from `git-pr` to `patchbin`, including the binary (`cmd/git-pr` to `cmd/patchbin`), default config file (`git-pr.toml` to `patchbin.toml`), and stylesheet (`static/git-pr.css` to `static/patchbin.css`)
- Replaced the permission model: PRs now have only two states, `draft` and `open` (draft is hidden from RSS feeds, open is not)
- Removed the single/multi-tenant concept in favor of one paradigm: anyone can submit PRs to any repo, but only admins own repos
- Anyone can submit patchsets on top of a PR for collaboration; there is no separate review step, only stacked patchsets
- `git am` now requires `--keep-empty` (or `git config --global am.keepEmpty true`) to retain cover letter commits

### Removed

- All repo/user index pages (to be reintroduced later)
- `create_repo` config field, which gated who could create repos (`admin` vs `user`); anyone can now create repos under the anonymous model
- Replaced `pr-X` and `ps-Y` with `[prID].[rev]` to remove confusion around the different ids

### Fixed

## v2026-02-25

### Added

- Ability to delete repo `ssh pr.pico.sh repo rm {name}`, must provide `--write` to persist

### Changed

- Replaced `--comment` flag which was a string into a bool and now require comment to be provided by stdin for commands `accept`, `close`, and `reopen`
  - `echo "lgtm!" | ssh pr.pico.sh pr accept --comment 100`
  - If no `--comment` flag provided then you don't need to provide stdin

### Fixed

- Mangled formatting for `ssh pr.pico.sh` help text

## v2026-02-24

### Changed

- Added `ssh {username}@pr register` command and now require explicit registration to use this service
- Upgraded to `go1.25`
- Removed charm's `wish` with pico's `pssh`
