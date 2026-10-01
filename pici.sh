#/usr/bin/env bash
set -euo pipefail

export ZMX_SESSION_PREFIX="${ZMX_SESSION_PREFIX:-local.patchbin.}"
JOB_ID="${PICI_JOB:-local}"
EVENT_TYPE="${PICI_EVENT:-manual}"

printf "\x1b[33m[%s] running ci (event=%s)\x1b[0m\n" "$JOB_ID" "$EVENT_TYPE"

zmx run test make check

printf "\x1b[32msuccess tests!\x1b[0m\n"

if [ "$EVENT_TYPE" != "git.tag" ]; then
  exit 0
fi

DOCKER_TAG="latest"
DOCKER_PLATFORM="linux/amd64,linux/arm64"

docker buildx ls | grep patchbin || docker buildx create --name patchbin
docker buildx use patchbin
zmx run release docker buildx build --push --platform "$DOCKER_PLATFORM" -t "ghcr.io/picosh/patchbin:$DOCKER_TAG" .

printf "\x1b[32msuccess release!\x1b[0m\n"
