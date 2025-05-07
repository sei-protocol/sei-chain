# Code generation

## Notes: 1/16/2025

To run the repo you need to use go 1.21, but to compile the proto files you need to use go 1.23. This means you need to use gvm to switch between the two versions and then modify the go.mod file to use the correct version.

Here is the command to do this and then switch back to 1.21 and run the tests:

```
git ls-files -m '*.pb.go' '*.pb.gw.go' | xargs git checkout -- && git checkout -- go.mod && git checkout -- go.sum && gvm use go1.23 && sed -i '' 's/^go 1\.21$/go 1.23/' go.mod && ignite generate proto-go -y && gvm use go1.21 && git checkout -- go.mod && git checkout -- go.sum && make install && go test ./...
```

## Remaining notes:

To generate the code for the protobuf files, first install the `ignite` tool.
We need version v0.23.0, which is outdated, but works with the current version of the codebase.
Pull binaries from the [releases page](https://github.com/ignite/cli/releases/tag/v0.23.0) or install from source code 
following instructions.

Verify the installation by running `ignite version`:

```bash
% ignite version          
·
· 🛸 Ignite CLI v28.2.0 is available!
·
· To upgrade your Ignite CLI version, see the upgrade doc: https://docs.ignite.com/guide/install.html#upgrading-your-ignite-cli-installation
·
··

Ignite CLI version:     v0.23.0
Ignite CLI build date:  2022-07-24T18:17:44Z
Ignite CLI source hash: 64df9aef958b3e8bc04b40d9feeb03426075ea89
Your OS:                darwin
Your arch:              arm64
Your go version:        go version go1.22.0 darwin/arm64
Your uname -a:          Darwin 23.1.0 Darwin Kernel Version 23.1.0: Mon Oct  9 21:32:11 PDT 2023; root:xnu-10002.41.9~7/RELEASE_ARM64_T6030 arm64
Your cwd:               /repos/sei-chain
Is on Gitpod:           false

```
Then, to generate the code, run the following command:

```bash
ignite generate proto-go
```