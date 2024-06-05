# Swagger/OpenAPI docs generation

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
Updated swagger/openapi yml file will be available in the `docs/swagger/swagger.yml` directory.

To view the rendered OpenAPI docs, try the [Swagger UI](https://editor-next.swagger.io/) 

# Serving the OpenAPI docs on the node

To serve the latest docs, first we need to update static assets. 
Make sure you generated latest swagger.yml using `ignite` as explained in `Swagger/OpenAPI docs generation`.
To do that execute `./scrtipts/update-swagger-ui-statik.sh` script.

```bash
./scripts/update-swagger-ui-statik.sh
```
Now we need to enable the swagger docs serving. 
To do that, update `api.swagger` value to `true` in the `app.toml` file.

```toml
###############################################################################
###                           API Configuration                             ###
###############################################################################

[api]

# Enable defines if the API server should be enabled.
enable = true

# Swagger defines if swagger documentation should automatically be registered.
swagger = true
```
Once node is restarted, swagger docs will be available at `http://<node-ip>:<port>/swagger/`