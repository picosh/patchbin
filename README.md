# patchbin

A pastebin for patches, supercharged for git collaboration.

Contributions are designed to be anonymous: the quality of your work is what matters. No signup required, just connect with an SSH key.

The target project doesn't need to run patchbin for someone to submit a patch request against it. It works like a pull request, except both sides collaborate by sending rounds of patchsets: a contributor sends patches, a reviewer replies with their own patches on top, back and forth, as commits rather than comments. The result is a collaborative workspace built entirely out of patches. Reviewing means pulling the code down, not clicking through a diff viewer. Issues work the same way: an issue is just a patch request without any code attached yet, and anyone can follow up with a real patch request on top of it.

There's no accept or reject step. A patch request is simply active or inactive: active ones go inactive after 30 days without activity. When a reviewer is happy with the code, they pull it, merge it, and push upstream themselves; there's nothing to manage here beyond that.

## quickstart

Submit a patch request (starts as a draft, visible only to you):

```
git format-patch main --stdout | ssh {url} pr create {repo}
```

Open it so others can see it (also enables RSS notifications):

```
ssh {url} pr open {prID}
```

Checkout the latest patchset from a patch request:

```
ssh {url} print pr-{prID} | git am -3
```

Add a follow-up patchset (e.g. after addressing review comments):

```
git format-patch main --stdout | ssh {url} pr add {prID}
```

Help guide:

```
ssh {url} help
```

## commands

### pr - manage patch requests

- `pr create {repo}` - submit a new PR from stdin (starts as draft)
  ```
  git format-patch main --stdout | ssh {url} pr create {repo}
  ```
- `pr add {prID}` - add a new patchset to an existing PR from stdin
  ```
  git format-patch main --stdout | ssh {url} pr add {prID}
  ```
- `pr open {prID} [--comment]` - transition draft open, enables RSS notifications
  ```
  ssh {url} pr open {prID}
  ```
- `pr draft {prID} [--comment]` - transition open draft, disables RSS notifications
  ```
  ssh {url} pr draft {prID}
  ```
- `pr edit {prID} {title}` - rename a PR
  ```
  ssh {url} pr edit {prID} "new title"
  ```
- `pr summary {prID}` - show metadata, patchsets, and patches for a PR
  ```
  ssh {url} pr summary {prID}
  ```
- `pr ls [repo] [--draft|--open|--active|--inactive|--mine]` - list PRs
  ```
  ssh {url} pr ls {repo} --open
  ```

### issue - text-only patch requests

- `issue create {repo} [--title]` - submit a new issue from stdin (starts as open)
  ```
  echo "steps to reproduce..." | ssh {url} issue create {repo} --title "bug: crash on startup"
  ```

### ps - manage patchsets

- `ps rm {patchsetID}` - remove a patchset and its patches (creator only)
  ```
  ssh {url} ps rm ps-{patchsetID}
  ```

### print - print patches for checkout

- `print pr-{prID}` - print the latest patchset for a PR
  ```
  ssh {url} print pr-{prID} | git am -3
  ```
- `print ps-{patchsetID}` - print a specific patchset
  ```
  ssh {url} print ps-{patchsetID} | git am -3
  ```

### logs - event history

- `logs [--pr ID] [--pubkey]` - list event logs, optionally filtered to a PR or your own activity
  ```
  ssh {url} logs --pr {prID}
  ```

## self-hosting

patchbin needs a `patchbin.toml` config file and a data directory (for the sqlite db and SSH host keys).

[Copy](./patchbin.toml) or create a `patchbin.toml` file inside a `./data` directory:

```
mkdir -p data
cp patchbin.toml ./data/patchbin.toml
vim ./data/patchbin.toml
```

### docker-compose

The included `docker-compose.yml` pulls the published image and mounts a local data directory:

```
services:
  patchbin:
    image: ghcr.io/picosh/pico/patchbin:latest
    restart: always
    volumes:
      - ./data/patchbin/data:/app/data
```

Place `patchbin.toml` inside `./data/patchbin/data`, then run:

```
docker compose up -d
```

### docker image

Run the image directly, mounting your data directory to `/app/data`:

```
docker run -d -v ./data:/app/data ghcr.io/picosh/pico/patchbin:latest
```

`patchbin.toml` must live inside the mounted `./data` directory, since that's the default config path the binary looks for.

### from go source

Clone the repo, then build and run the binary:

```
make build
./build/patchbin --config ./data/patchbin.toml
```

Or without the Makefile:

```
go build -o ./build/patchbin ./cmd/patchbin
./build/patchbin --config ./data/patchbin.toml
```

### done

Access the SSH app:

```
ssh -p 2222 localhost help
```

Access the web app:

```
curl localhost:3000
```
