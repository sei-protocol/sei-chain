"""Helpers for fetching commit authors from GitHub."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Dict, Iterable, Optional

import logging
import os

try:
    import requests
except ImportError:  # pragma: no cover - requests is part of std deps for runtime
    requests = None  # type: ignore

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class CommitAuthor:
    """Simple representation of a commit author."""

    identifier: str
    source: str


def _extract_commit_author_details(payload: Dict) -> Optional[CommitAuthor]:
    """Extract the most reliable author details from a GitHub commit payload."""

    author = payload.get("author") or {}
    if isinstance(author, dict):
        login = author.get("login")
        if login:
            return CommitAuthor(login, "author")
        name = author.get("name")
        if name:
            return CommitAuthor(name, "author")

    commit = payload.get("commit") or {}
    if isinstance(commit, dict):
        commit_author = commit.get("author") or {}
        if isinstance(commit_author, dict):
            login = commit_author.get("login")
            if login:
                return CommitAuthor(login, "commit.author")
            name = commit_author.get("name")
            if name:
                return CommitAuthor(name, "commit.author")

        commit_committer = commit.get("committer") or {}
        if isinstance(commit_committer, dict):
            login = commit_committer.get("login")
            if login:
                return CommitAuthor(login, "commit.committer")
            name = commit_committer.get("name")
            if name:
                return CommitAuthor(name, "commit.committer")

    committer = payload.get("committer") or {}
    if isinstance(committer, dict):
        login = committer.get("login")
        if login:
            return CommitAuthor(login, "committer")
        name = committer.get("name")
        if name:
            return CommitAuthor(name, "committer")

    return None


def _normalise_repo(repo: str) -> str:
    """Normalise a GitHub repo string to the form "owner/name"."""

    repo = repo.strip()
    if repo.endswith("/"):
        repo = repo[:-1]
    if repo.startswith("https://github.com/"):
        repo = repo[len("https://github.com/") :]
    repo = repo.strip("/")
    return repo


class GitHubSourceControlHistoryItemDetailsProvider:
    """Fetch commit information from GitHub."""

    _BASE_URL = "https://api.github.com/repos/{repo}/commits/{sha}"

    def __init__(self, *, session: Optional["requests.Session"] = None, token: Optional[str] = None):
        if session is not None:
            self._session = session
        else:
            if requests is None:
                raise RuntimeError("The requests package is required to use the provider.")
            self._session = requests.Session()
        self._token = token or os.getenv("GITHUB_TOKEN")

    def _headers(self) -> Dict[str, str]:
        headers = {"Accept": "application/vnd.github+json"}
        if self._token:
            headers["Authorization"] = f"Bearer {self._token}"
        return headers

    def get_commit_author_details(self, repo: str, sha: str) -> Optional[CommitAuthor]:
        repo = _normalise_repo(repo)
        url = self._BASE_URL.format(repo=repo, sha=sha)
        try:
            response = self._session.get(url, headers=self._headers(), timeout=10)
            response.raise_for_status()
        except Exception as exc:  # pragma: no cover - network failures handled uniformly
            logger.debug("Failed to fetch commit %s@%s: %s", repo, sha, exc)
            return None
        try:
            payload = response.json()
        except ValueError:
            logger.debug("Invalid JSON for commit %s@%s", repo, sha)
            return None
        return _extract_commit_author_details(payload)

    def get_commit_authors(self, repo: str, shas: Iterable[str]) -> Dict[str, Optional[CommitAuthor]]:
        repo = _normalise_repo(repo)
        results: Dict[str, Optional[CommitAuthor]] = {}
        for sha in shas:
            results[sha] = self.get_commit_author_details(repo, sha)
        return results
