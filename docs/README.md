# OpenAPI docs generation

To generate the [OpenAPI](https://github.com/OAI/OpenAPI-Specification/) docs, first install the `ignite` tool.
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
....

```
Then, to generate the OpenAPI docs, run the following command:

```bash
ignite generate openapi
```
Updated docs will be available in the `docs/static/openapi.yml` directory.

To view the rendered OpenAPI docs, try the [Swagger UI](https://editor-next.swagger.io/) 