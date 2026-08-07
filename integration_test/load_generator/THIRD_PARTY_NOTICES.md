# Third-party notices

## SushiSwap V2

The files under `contracts/uniswapv2/` and the canonical deployment artifacts under
`vendor/sushiswap-v2/` are from the official SushiSwap repository:

- Repository: <https://github.com/sushiswap/sushiswap>
- Commit: `94ea7712daaa13155dfab9786aacf69e24390147`
- License: GNU General Public License v3.0

The complete upstream license is retained at `contracts/uniswapv2/LICENSE`. The
canonical factory and router artifacts are the official mainnet deployment artifacts
recorded at that commit. The pair creation bytecode is the verified Ethereum production
bytecode whose init-code hash is
`0xe18a34eb0e04b04f7a0ac29a6e80748dca96319b42c54d679cb821dca90c6303`.
See `vendor/sushiswap-v2/PROVENANCE.json` for file hashes and compiler details.

`contracts/mocks/WETH9.sol` is retained unchanged from the official SushiSwap V2 core
repository at commit `a349882c918c752d0363e4c6e02dedcbc48b734e` and is licensed
under GPL-3.0-only as stated in that file.

These GPL-covered Solidity sources and their compiled artifacts remain subject to the
GPL. The surrounding load-generator code continues to use its existing project license;
this notice does not attempt to relicense the third-party material.
