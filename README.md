# patchbin

A pastebin for patches, supercharged for git collaboration.

Contributions are designed to be anonymous: the quality of your work is what matters. No signup required, just connect with an SSH key.

The target project doesn't need to run patchbin for someone to submit a patch request against it. It works like a pull request, except both sides collaborate by sending rounds of patchsets: a contributor sends patches, a reviewer replies with their own patches on top, back and forth, as commits rather than comments. The result is a collaborative workspace built entirely out of patches. Reviewing means pulling the code down, not clicking through a diff viewer. Issues work the same way: an issue is just a patch request without any code attached yet, and anyone can follow up with a real patch request on top of it.

There's no accept or reject step. A patch request is simply active or inactive: active ones go inactive after 14 days without activity. When a reviewer is happy with the code, they pull it, merge it, and push upstream themselves; there's nothing to manage here beyond that.

## quickstart

Submit a patch request (new or follow-up):

```
git format-patch main --stdout | ssh {url} {repo}:{slug}
```

Checkout the latest patchset from a patch request:

```
ssh {url} pull {repo}:{slug} | git am -3
# or shorthand:
ssh {url} {repo}:{slug}.patch | git am -3
```

View PR metadata and discussion:

```
ssh {url} show {repo}:{slug}
```

Help guide:

```
ssh {url} help
```

## commands

### {repo}:{slug} - submit a patchset

Submit a new PR or follow-up patchset from stdin:
```
git format-patch main --stdout | ssh {url} {repo}:{slug}
```

### pull - print patches for checkout

- `pull {repo}:{slug} [rev]` - print mbox patchset for checkout (pipes to git am)
  ```
  ssh {url} pull {repo}:{slug} | git am -3
  ```
- `{repo}:{slug}.patch` - shorthand to pull latest patchset
  ```
  ssh {url} {repo}:{slug}.patch | git am -3
  ```

### show - view PR summary

- `show {repo}:{slug}` - show metadata, patchsets, and patches for a PR
  ```
  ssh {url} show {repo}:{slug}
  ```

### ls - list patch requests

- `ls [repo] [--active|--inactive|--mine]` - list PRs
  ```
  ssh {url} ls {repo}
  ```

### comment - add a comment

- `comment {repo}:{slug} [message]` - add a comment via argument or stdin
  ```
  ssh {url} comment {repo}:{slug} "looks good to me!"
  echo "looks good to me!" | ssh {url} comment {repo}:{slug}
  ```

### edit - rename a PR

- `edit {repo}:{slug} {title}` - rename a PR (creator only)
  ```
  ssh {url} edit {repo}:{slug} "new title"
  ```

### rm - remove a patchset

- `rm {repo}:{slug}.{rev}` - remove a patchset and its patches (creator only)
  ```
  ssh {url} rm {repo}:{slug}.2
  ```

### issue - text-only patch requests

- `issue {repo}:{slug} [title] [body]` - submit a new issue
  ```
  ssh {url} issue {repo}:{slug} "bug: crash on startup" "steps to reproduce..."
  ```

### logs - event history

- `logs [--pr {repo}:{slug}] [--pubkey]` - list event logs, optionally filtered to a PR or your own activity
  ```
  ssh {url} logs --pr {repo}:{slug}
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
