# Contributing

- [Contributing](#contributing)
  - [Architecture Decision Records (ADR)](#architecture-decision-records-adr)
  - [Pull Requests](#pull-requests)
    - [Process for reviewing PRs](#process-for-reviewing-prs)
    - [Updating Documentation](#updating-documentation)
  - [Forking](#forking)
  - [Dependencies](#dependencies)
  - [Protobuf](#protobuf)
  - [Testing](#testing)
  - [Branching Model and Release](#branching-model-and-release)
    - [PR Targeting](#pr-targeting)
    - [Development Procedure](#development-procedure)
    - [Pull Merge Procedure](#pull-merge-procedure)
    - [Release Procedure](#release-procedure)
    - [Point Release Procedure](#point-release-procedure)

Thank you for considering making contributions to ibc-go! 

Contributing to this repo can mean many things such as participating in
discussion or proposing code changes. To ensure a smooth workflow for all
contributors, the general procedure for contributing has been established:

1. Either [open](https://github.com/cosmos/ibc-go/issues/new/choose) or
   [find](https://github.com/cosmos/ibc-go/issues) an issue you'd like to help with
2. Participate in thoughtful discussion on that issue
3. If you would like to contribute:
   1. If the issue is a proposal, ensure that the proposal has been accepted
   2. Ensure that nobody else has already begun working on this issue. If they have,
      make sure to contact them to collaborate
   3. If nobody has been assigned for the issue and you would like to work on it,
      make a comment on the issue to inform the community of your intentions
      to begin work
   4. Follow standard Github best practices: fork the repo, branch from the
      HEAD of `main`, make some commits, and submit a PR to `main`
      - For core developers working within the ibc-go repo, to ensure a clear
        ownership of branches, branches must be named with the convention
        `{moniker}/{issue#}-branch-name`
   5. Be sure to submit the PR in `Draft` mode submit your PR early, even if
      it's incomplete as this indicates to the community you're working on
      something and allows them to provide comments early in the development process
   6. When the code is complete it can be marked `Ready for Review`
   7. Be sure to include a relevant change log entry in the `Unreleased` section
      of `CHANGELOG.md` (see file for log format)

Note that for very small or blatantly obvious problems (such as typos) it is
not required to an open issue to submit a PR, but be aware that for more complex
problems/features, if a PR is opened before an adequate design discussion has
taken place in a github issue, that PR runs a high likelihood of being rejected.

Other notes:

- Looking for a good place to start contributing? How about checking out some
  [good first issues](https://github.com/cosmos/ibc-go/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22)
- Please make sure to run `make format` before every commit - the easiest way
  to do this is have your editor run it for you upon saving a file. Additionally
  please ensure that your code is lint compliant by running `golangci-lint run`.

## Architecture Decision Records (ADR)

When proposing an architecture decision for the ibc-go, please create an [ADR](./docs/architecture/README.md)
so further discussions can be made. We are following this process so all involved parties are in
agreement before any party begins coding the proposed implementation. If you would like to see some examples
of how these are written refer to [Cosmos SDK ADRs](https://github.com/cosmos/cosmos-sdk/tree/master/docs/architecture)

## Pull Requests

To accommodate review process we suggest that PRs are categorically broken up.
Ideally each PR addresses only a single issue. Additionally, as much as possible
code refactoring and cleanup should be submitted as a separate PRs from bugfixes/feature-additions.

### Process for reviewing PRs

All PRs require an approval from at least one CODEOWNER before merge. PRs which cause signficant changes require two approvals from CODEOWNERS. When reviewing PRs please use the following review explanations:

- `LGTM` without an explicit approval means that the changes look good, but you haven't pulled down the code, run tests locally and thoroughly reviewed it.
- `Approval` through the GH UI means that you understand the code, documentation/spec is updated in the right places, you have pulled down and tested the code locally. In addition:
  - You must also think through anything which ought to be included but is not
  - You must think through whether any added code could be partially combined (DRYed) with existing code
  - You must think through any potential security issues or incentive-compatibility flaws introduced by the changes
  - Naming must be consistent with conventions and the rest of the codebase
  - Code must live in a reasonable location, considering dependency structures (e.g. not importing testing modules in production code, or including example code modules in production code).
  - if you approve of the PR, you are responsible for fixing any of the issues mentioned here and more
- If you sat down with the PR submitter and did a pairing review please note that in the `Approval`, or your PR comments.
- If you are only making "surface level" reviews, submit any notes as `Comments` without adding a review.

### Updating Documentation

If you open a PR on ibc-go, it is mandatory to update the relevant documentation in /docs.

## Forking

Please note that Go requires code to live under absolute paths, which complicates forking.
While my fork lives at `https://github.com/colin-axner/ibc-go`,
the code should never exist at `$GOPATH/src/github.com/colin-axner/ibc-go`.
Instead, we use `git remote` to add the fork as a new remote for the original repo,
`$GOPATH/src/github.com/cosmos/ibc-go`, and do all the work there.

For instance, to create a fork and work on a branch of it, I would:

- Create the fork on github, using the fork button.
- Go to the original repo checked out locally (i.e. `$GOPATH/src/github.com/cosmos/ibc-go`)
- `git remote add fork git@github.com:colin-axner/ibc-go.git`

Now `fork` refers to my fork and `origin` refers to the ibc-go version.
So I can `git push -u fork main` to update my fork, and make pull requests to ibc-go from there.
Of course, replace `colin-axner` with your git handle.

To pull in updates from the origin repo, run

- `git fetch origin`
- `git rebase origin/main` (or whatever branch you want)

Please don't make Pull Requests from `main`.

## Dependencies

We use [Go 1.14 Modules](https://github.com/golang/go/wiki/Modules) to manage
dependency versions.

The main branch of every Cosmos repository should just build with `go get`,
which means they should be kept up-to-date with their dependencies, so we can
get away with telling people they can just `go get` our software.

Since some dependencies are not under our control, a third party may break our
build, in which case we can fall back on `go mod tidy -v`.

## Protobuf

We use [Protocol Buffers](https://developers.google.com/protocol-buffers) along with [gogoproto](https://github.com/gogo/protobuf) to generate code for use in ibc-go.

For determinstic behavior around Protobuf tooling, everything is containerized using Docker. Make sure to have Docker installed on your machine, or head to [Docker's website](https://docs.docker.com/get-docker/) to install it.

For formatting code in `.proto` files, you can run `make proto-format` command.

For linting and checking breaking changes, we use [buf](https://buf.build/). You can use the commands `make proto-lint` and `make proto-check-breaking` to respectively lint your proto files and check for breaking changes.

To generate the protobuf stubs, you can run `make proto-gen`.

We also added the `make proto-all` command to run all the above commands sequentially.

In order for imports to properly compile in your IDE, you may need to manually set your protobuf path in your IDE's workspace settings/config.

For example, in vscode your `.vscode/settings.json` should look like:

```
{
    "protoc": {
        "options": [
        "--proto_path=${workspaceRoot}/proto",
        "--proto_path=${workspaceRoot}/third_party/proto"
        ]
    }
}
```

## Testing

All go tests in ibc-go can be ran by running `make test`.

When testing a function under a variety of different inputs, we prefer to use
[table driven tests](https://github.com/golang/go/wiki/TableDrivenTests).

All tests should use the testing package. Please see the testing package [README](./testing/README.md) for more information.


## Branching Model and Release

User-facing repos should adhere to the trunk based development branching model: https://trunkbaseddevelopment.com/.

ibc-go utilizes [semantic versioning](https://semver.org/).

### PR Targeting

Ensure that you base and target your PR on the `main` branch.

All development should be targeted against `main`. Bug fixes which are required for outstanding releases should be backported if the CODEOWNERS decide it is applicable. 

### Development Procedure

- the latest state of development is on `main`
- `main` must never fail `make test`
- no `--force` onto `main` (except when reverting a broken commit, which should seldom happen)
- create a development branch either on github.com/cosmos/ibc-go, or your fork (using `git remote add fork`)
- before submitting a pull request, begin `git rebase` on top of `main`

### Pull Merge Procedure

- ensure all github requirements pass
- squash and merge pull request 

### Release Procedure

- Start on `main`
- Create the release candidate branch `rc/v*` (going forward known as **RC**)
  and ensure it's protected against pushing from anyone except the release
  manager/coordinator
  - **no PRs targeting this branch should be merged unless exceptional circumstances arise**
- On the `RC` branch, prepare a new version section in the `CHANGELOG.md`
  - All links must be link-ified: `$ python ./scripts/linkify_changelog.py CHANGELOG.md`
- Run external relayer tests against the prepared RC
- If errors are found during the relayer testing, commit the fixes to `main`
  and create a new `RC` branch (making sure to increment the `rcN`)
- After relayer testing has successfully completed, create the release branch
  (`release/vX.XX.X`) from the `RC` branch
- Create a PR to `main` to incorporate the `CHANGELOG.md` updates
- Tag the release (use `git tag -a`) and create a release in Github
- Delete the `RC` branches

### Point Release Procedure

At the moment, only a single major release will be supported, so all point releases will be based
off of that release.

In order to alleviate the burden for a single person to have to cherry-pick and handle merge conflicts
of all desired backporting PRs to a point release, we instead maintain a living backport branch, where
all desired features and bug fixes are merged into as separate PRs.

Example:

Current release is `v1.0.2`. We then maintain a (living) branch `release/v1.0.x`, given x as
the next patch release number (currently `1.0.3`) for the `1.0` release series. As bugs are fixed
and PRs are merged into `main`, if a contributor wishes the PR to be released into the
`v1.0.x` point release, the contributor must:

1. Add `1.0.x-backport` label
2. Pull latest changes on the desired `release/v1.0.x` branch
3. Create a 2nd PR merging the respective PR into `release/v1.0.x`
4. Update the PR's description and ensure it contains the following information:
   - **[Impact]** Explanation of how the bug affects users or developers.
   - **[Test Case]** section with detailed instructions on how to reproduce the bug.
   - **[Regression Potential]** section with a discussion how regressions are most likely to manifest, or might
     manifest even if it's unlikely, as a result of the change. **It is assumed that any backport PR is
     well-tested before it is merged in and has an overall low risk of regression**. This section should discuss
     the potential for state breaking changes to occur such as through out-of-gas errors. 

It is the PR's author's responsibility to fix merge conflicts, update changelog entries, and
ensure CI passes. If a PR originates from an external contributor, it may be a core team member's
responsibility to perform this process instead of the original author.
Lastly, it is core team's responsibility to ensure that the PR meets all the backport criteria.

Finally, when a point release is ready to be made:

1. Checkout `release/v1.0.x` branch
2. Ensure changelog entries are verified
3. Add release version date to the changelog
4. Push release branch along with the annotated tag: **git tag -a**
5. Create a PR into `main` containing ONLY `CHANGELOG.md` updates

Note, although we aim to support only a single release at a time, the process stated above could be
used for multiple previous versions.
