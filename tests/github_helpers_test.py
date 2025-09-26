"""Robust tests for GitHub attribution and commit author resolution."""
from __future__ import annotations

import pytest
from unittest.mock import MagicMock

from claim_kin_agent_attribution.github_helpers import (
    CommitAuthor,
    _extract_commit_author_details,
    _normalise_repo,
    GitHubSourceControlHistoryItemDetailsProvider,
)


# ----------------------------------------------------------------------
# Core logic: _extract_commit_author_details()
# ----------------------------------------------------------------------

def test_extract_commit_author_prefers_login_over_name():
    payload = {"author": {"login": "octocat", "name": "The Octocat"}}
    result = _extract_commit_author_details(payload)
    assert result == CommitAuthor("octocat", "author")


def test_extract_commit_author_fallbacks_order():
    # commit.committer.name fallback path
    payload = {
        "commit": {
            "committer": {
                "name": "Bob Builder"
            }
        }
    }
    result = _extract_commit_author_details(payload)
    assert result == CommitAuthor("Bob Builder", "commit.committer")


def test_extract_commit_author_empty_payload_returns_none():
    result = _extract_commit_author_details({})
    assert result is None


# ----------------------------------------------------------------------
# Repo normalizer: _normalise_repo()
# ----------------------------------------------------------------------

@pytest.mark.parametrize("input_repo, expected", [
    ("https://github.com/user/repo", "user/repo"),
    ("https://github.com/user/repo/", "user/repo"),
    ("user/repo", "user/repo"),
    ("user/repo/", "user/repo"),
    ("/user/repo/", "user/repo"),
])
def test_repo_normalisation(input_repo, expected):
    assert _normalise_repo(input_repo) == expected


# ----------------------------------------------------------------------
# GitHub API wrapper logic: GitHubSourceControlHistoryItemDetailsProvider
# ----------------------------------------------------------------------


def make_fake_response(payload: dict):
    class FakeResponse:
        def raise_for_status(self):
            pass

        def json(self):
            return payload

    return FakeResponse()


def test_provider_returns_correct_author_from_author_login():
    payload = {"author": {"login": "octocat"}}
    session = MagicMock()
    session.get.return_value = make_fake_response(payload)

    provider = GitHubSourceControlHistoryItemDetailsProvider(session=session)
    author = provider.get_commit_author_details("octocat/Hello-World", "abc123")

    assert isinstance(author, CommitAuthor)
    assert author.identifier == "octocat"
    assert author.source == "author"


def test_provider_handles_commit_author_name():
    payload = {
        "commit": {
            "author": {
                "name": "Alice Wonderland"
            }
        }
    }
    session = MagicMock()
    session.get.return_value = make_fake_response(payload)

    provider = GitHubSourceControlHistoryItemDetailsProvider(session=session)
    author = provider.get_commit_author_details("org/repo", "def456")

    assert author == CommitAuthor("Alice Wonderland", "commit.author")


def test_provider_handles_missing_author_fields_gracefully():
    payload = {"commit": {"message": "no author info"}}
    session = MagicMock()
    session.get.return_value = make_fake_response(payload)

    provider = GitHubSourceControlHistoryItemDetailsProvider(session=session)
    author = provider.get_commit_author_details("user/repo", "noauth123")

    assert author is None


def test_provider_handles_api_error_and_logs(monkeypatch):
    session = MagicMock()
    session.get.side_effect = Exception("API down")

    provider = GitHubSourceControlHistoryItemDetailsProvider(session=session)
    author = provider.get_commit_author_details("broken/repo", "deadbeef")

    assert author is None


def test_provider_batch_get_commit_authors():
    payloads = {
        "sha1": {"author": {"login": "octocat"}},
        "sha2": {"commit": {"committer": {"name": "Builder Bob"}}},
        "sha3": {},  # Will be None
    }

    session = MagicMock()

    def mock_get(url, headers=None, timeout=10):
        if "sha1" in url:
            return make_fake_response(payloads["sha1"])
        if "sha2" in url:
            return make_fake_response(payloads["sha2"])
        if "sha3" in url:
            return make_fake_response(payloads["sha3"])
        raise Exception("Unknown SHA")

    session.get.side_effect = mock_get

    provider = GitHubSourceControlHistoryItemDetailsProvider(session=session)
    results = provider.get_commit_authors("org/repo", ["sha1", "sha2", "sha3"])

    assert results["sha1"] == CommitAuthor("octocat", "author")
    assert results["sha2"] == CommitAuthor("Builder Bob", "commit.committer")
    assert results["sha3"] is None
