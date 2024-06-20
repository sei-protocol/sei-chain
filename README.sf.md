## StreamingFast Sei Fork Notes

### Remotes & Branches

```bash
git remote set-url origin https://github.com/sei-protocol/sei-chain.git
git remote add sf git@github.com:streamingfast/sei-chain.git
```

We maintain 3 branches:

- `feature/firehose-tracer`
- `feature/firehose-tracer-at-latest-release-tag`
- `release/firehose`

The `release/firehose` contains Dockerfile and instructions how to build images, this is the branch that should be used to make releases.

The `feature/firehose-tracer` is the PR branch that tracks `origin/main` branch, `feature/firehose-tracer-at-latest-release-tag` is the same as `feature/firehose-tracer` but tracks the latest release tag, which as of time of writing is `v5.5.5`.

### Bumping to new version

```bash
git fetch origin

# Found correct tag to bump to, we will use `v5.5.6`
export VERSION=v5.5.6

git checkout feature/firehose-tracer-at-latest-release-tag
git pull

git merge "${VERSION:?}"
# Fix any conflicts
go test ./...
git commit

git checkout release/firehose
git pull

git merge feature/firehose-tracer-at-latest-release-tag
# Fix any conflicts and merge, but there is usually no conflicts in this step

git tag "${VERSION:?}-fh3.0"

git push feature/firehose-tracer-at-latest-release-tag release/firehose "${VERSION:?}-fh3.0"
```

#### Building Binary & Docker

Built manually for now on the GCP VM, here the commands we use to build it in our VM.

> [!NOTE]
> The instructions below **must** be run on the VM itself for now.

```bash
export SEID_REF=v5.5.5-fh3.0 \
&& sudo -u sei git -C /data/build/seid/ fetch origin \
&& sudo -u sei git -C /data/build/seid/ checkout "${SEID_REF:?}" \
&& sudo -u sei bash -c 'source /etc/profile.d/02-golang.sh && cd /data/build/seid && make install' \
&& sudo cp /home/sei/go/bin/seid /usr/local/bin/seid-"${SEID_REF:?}"
```

Then from your developer machine now, run the following commands which download from the VM the binary locally and then build a Docker image from it.

Adjust the `TAG` export to use a repository you control, the `SEID_REF` to fit with the correct version.

```
# Assumed to be in `sei-chain` root folder, replace `sei0` in scp command to fit your own machine's name

export SEID_REF=v5.5.6-fh3.0 \
&& export FIREETH=v2.6.2 \
&& export TAG="ghcr.io/streamingfast/firehose-ethereum:${FIREETH:?}-sei-${SEID_REF:?}" \
&& scp sei0:/usr/local/bin/seid-${SEID_REF:?} . \
&& docker build --platform=linux/amd64 --build-arg="FIREETH=${FIREETH:?}" --build-arg="SEID_BIN=seid-${SEID_REF:?}" -t "${TAG:?}" -f Dockerfile.sf . \
&& docker run --platform=linux/amd64 --rm -it "${TAG:?}" seid version \
&& docker push "${TAG:?}"
```
