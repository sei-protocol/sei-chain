import { ReplayDeploymentManifest } from './replay/replayTypes';

export const SUSHI_V2_PROVENANCE = {
    repository: 'https://github.com/sushiswap/sushiswap',
    commit: '94ea7712daaa13155dfab9786aacf69e24390147',
    pairInitCodeHash: '0xe18a34eb0e04b04f7a0ac29a6e80748dca96319b42c54d679cb821dca90c6303',
    factoryCreationCodeHash:
        '0xb6cfa36fab05ca5b0f79d18c0e80ad4a0158030127c669efb506ba7dec6ad3ca',
    routerCreationCodeHash:
        '0x242b3e2b1fea435b76ad7353f445b0e570d3117d6876eaf4b141790b90c0cc1a',
} as const;

export function validateSushiV2Provenance(manifest: ReplayDeploymentManifest): void {
    const actual = manifest.sushiV2;
    if (
        actual?.repository !== SUSHI_V2_PROVENANCE.repository ||
        actual.commit !== SUSHI_V2_PROVENANCE.commit ||
        actual.pairInitCodeHash !== SUSHI_V2_PROVENANCE.pairInitCodeHash ||
        actual.factoryCreationCodeHash !== SUSHI_V2_PROVENANCE.factoryCreationCodeHash ||
        actual.routerCreationCodeHash !== SUSHI_V2_PROVENANCE.routerCreationCodeHash
    ) {
        throw new Error('Replay deployment SushiSwap V2 provenance is missing or incompatible');
    }
}
