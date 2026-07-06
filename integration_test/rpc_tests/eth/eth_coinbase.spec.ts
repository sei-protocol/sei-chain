import { ethers } from "ethers";
import { expect } from "chai";
import { fromBech32 } from "@cosmjs/encoding";

import { seiRpc } from "../utils/chainUtils";
import { readRuntimeState } from "../utils/testUtils";
import { claimPool } from "../utils/testUtils";
import { isSeiDocker, seiAddressFromMnemonic, feeCollectorCosmosAddress } from "../utils/cosmosUtils";
import { bankBalanceUsei } from "../utils/cosmosUtils";
import { rawSei, rawGeth, expectJsonRpcError } from "../utils/chainUtils";
import { WEI_PER_USEI, ZERO_ADDRESS } from "../utils/constants";

describe('eth_coinbase Tests', function () {
    this.timeout(120 * 1000);

    let seiProvider: ethers.JsonRpcProvider;
    let feeCollectorAddr: string;

    before(async () => {
        seiProvider = seiRpc();
        const { adminMnemonic } = readRuntimeState().funded;
        const { prefix } = fromBech32(await seiAddressFromMnemonic(adminMnemonic));
        feeCollectorAddr = feeCollectorCosmosAddress(prefix);
    });

    it('eth_coinbase returns a syntactically valid 20-byte EVM address', async () => {
        const coinbase = await seiProvider.send('eth_coinbase', []);
        expect(coinbase).to.match(/^0x[0-9a-fA-F]{40}$/);
        expect(coinbase.toLowerCase()).to.not.equal(ZERO_ADDRESS);
    });

    it('eth_coinbase is distinct from block.coinbase (the per-block proposer)', async () => {
        const [coinbase, block] = await Promise.all([
            seiProvider.send('eth_coinbase', []),
            seiProvider.send('eth_getBlockByNumber', ['latest', false]),
        ]);
        expect(block.miner).to.match(/^0x[0-9a-fA-F]{40}$/);
        expect(coinbase.toLowerCase()).to.not.equal(block.miner.toLowerCase());
    });

    it('eth_coinbase equals the EVM-mapped address of the cosmos fee_collector module account', async () => {
        const coinbase = (await seiProvider.send('eth_coinbase', [])).toLowerCase();

        const evmAddress: string = await seiProvider.send('sei_getEVMAddress', [feeCollectorAddr]);
        expect(evmAddress.toLowerCase()).to.equal(coinbase);
    });

    it('eth_coinbase round-trips: sei_getSeiAddress(coinbase) equals the derived fee_collector address', async () => {
        const coinbase = await seiProvider.send('eth_coinbase', []);

        const seiAddress: string = await seiProvider.send('sei_getSeiAddress', [coinbase]);
        expect(seiAddress).to.equal(feeCollectorAddr);
    });

    it('EVM tx fees accrue to eth_coinbase (the fee_collector) and are swept each block', async function () {
        if (!(await isSeiDocker())) this.skip();

        const coinbase = (await seiProvider.send('eth_coinbase', [])).toLowerCase();
        const [signer] = claimPool(readRuntimeState(), seiProvider, 1, 'eth_coinbase');

        const gasPrice = BigInt(await seiProvider.send('eth_gasPrice', []));
        const tip = ethers.parseUnits('2', 'gwei');
        const tx = await signer.wallet.sendTransaction({
            to: signer.address,
            value: 0n,
            maxFeePerGas: gasPrice * 3n + tip,
            maxPriorityFeePerGas: tip,
        });
        const receipt = await tx.wait(1, 30_000);
        const blockN = receipt!.blockNumber;
        const ourFeeWei = receipt!.gasUsed * receipt!.gasPrice!;

        const balN = await bankBalanceUsei(feeCollectorAddr, blockN);
        expect(balN * WEI_PER_USEI >= ourFeeWei).to.equal(
            true,
            `fee_collector at height ${blockN} (${balN} usei) must include our ${ourFeeWei} wei fee`,
        );
    });

    it('rejects extra parameters on Sei with -32602 and geths exact message', async () => {
        const [positional, object] = await Promise.all([
            rawSei('eth_coinbase', ['latest']),
            rawSei('eth_coinbase', [{}]),
        ]);
        expectJsonRpcError(positional, -32602, /^too many arguments, want at most 0$/);
        expectJsonRpcError(object, -32602, /^too many arguments, want at most 0$/);
    });
});
