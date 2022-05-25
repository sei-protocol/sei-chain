# seichain
**seichain** is a blockchain built using Cosmos SDK and Tendermint and created with [Starport](https://starport.com).

## Get started
You may use starport to run the chain, but typically we have our own customizations that require using an internal tool (seid). Both methods are shown below.
### Starport

```
starport chain serve
```

`serve` command installs dependencies, builds, initializes, and starts your blockchain in development.

### Internal tool
First build the tool
```
make install
```

If you've run the chain before, you may have leftover cruft. Run the following to reset the state.
```
seid unsafe-reset-all
```

Next, initialize the chain. This creates the genesis field:
```
seid init {moniker} --chain-id sei-chain
```

Finally, start the chain:
```
seid start
```


### Configure

Your blockchain in development can be configured with `config.yml`. To learn more, see the [Starport docs](https://docs.starport.com).

### Web Frontend

Starport has scaffolded a Vue.js-based web app in the `vue` directory. Run the following commands to install dependencies and start the app:

```
cd vue
npm install
npm run serve
```

The frontend app is built using the `@starport/vue` and `@starport/vuex` packages. For details, see the [monorepo for Starport front-end development](https://github.com/tendermint/vue).

## Release
To release a new version of your blockchain, create and push a new tag with `v` prefix. A new draft release with the configured targets will be created.

```
git tag v0.1
git push origin v0.1
```

After a draft release is created, make your final changes from the release page and publish it.

### Install
To install the latest version of your blockchain node's binary, execute the following command on your machine:

```
curl https://get.starport.com/sei-protocol/sei-chain@latest! | sudo bash
```
`sei-protocol/sei-chain` should match the `username` and `repo_name` of the Github repository to which the source code was pushed. Learn more about [the install process](https://github.com/allinbits/starport-installer).

## Learn more

- [Starport](https://starport.com)
- [Tutorials](https://docs.starport.com/guide)
- [Starport docs](https://docs.starport.com)
- [Cosmos SDK docs](https://docs.cosmos.network)
- [Developer Chat](https://discord.gg/H6wGTY8sxw)
