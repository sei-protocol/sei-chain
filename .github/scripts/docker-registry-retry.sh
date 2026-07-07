#!/usr/bin/env bash
# Retry helpers for transient GHCR/docker registry errors (e.g. "unknown blob").

_registry_retry() {
  local op="$1"
  local ref="$2"
  local attempt
  for attempt in 1 2 3 4 5; do
    if docker "$op" "$ref"; then
      return 0
    fi
    echo "docker $op $ref failed (attempt $attempt), retrying in $((attempt * 5))s..."
    sleep $((attempt * 5))
  done
  echo "docker $op $ref failed after 5 attempts"
  return 1
}

push_with_retry() {
  _registry_retry push "$1"
}

pull_with_retry() {
  _registry_retry pull "$1"
}
