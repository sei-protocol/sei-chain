import { ethers } from "ethers";
import { expect } from "chai";
import { fromBech32 } from "@cosmjs/encoding";

import { seiRpc } from "../utils/chainUtils";
import { AdminMnemonic } from "../config/endpoints";
import { readRuntimeState } from "../utils/testUtils";
import { claimPool } from "../utils/testUtils";
import { isSeiDocker, seiAddressFromMnemonic, feeCollectorCosmosAddress } from "../utils/cosmosUtils";
import { bankBalanceUsei } from "../utils/cosmosUtils";
import { rawSei, rawGeth, expectJsonRpcError } from "../utils/chainUtils";
import { WEI_PER_USEI, ZERO_ADDRESS } from "../utils/constants";

describe('Eth Coinbase Rpc Tests', function () {
    this.timeout(120 * 1000);

    let seiProvider: ethers.JsonRpcProvider;
    let feeCollectorAddr: string;

    before(async () => {
        seiProvider = seiRpc();
        const { prefix } = fromBech32(await seiAddressFromMnemonic(AdminMnemonic));
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
        expect(evmAddress).to.match(
            /^0x[0-9a-fA-F]{40}$/,
            'fee_collector module account must be associated on a live Sei chain',
        );
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

        // The fee_collector holds at least our fee at the tx's height (>= leaves room for
        // other txs sharing the block under parallel runs); 1 usei == 1e12 wei.
        const balN = await bankBalanceUsei(feeCollectorAddr, blockN);
        expect(balN * WEI_PER_USEI >= ourFeeWei).to.equal(
            true,
            `fee_collector at height ${blockN} (${balN} usei) must include our ${ourFeeWei} wei fee`,
        );

        // Divergence from geth: the same fee never shows up on the EVM balance surface.
        const evmBalAtN = BigInt(
            await seiProvider.send('eth_getBalance', [coinbase, '0x' + blockN.toString(16)]),
        );
        expect(evmBalAtN, 'eth_getBalance must not surface the swept fee_collector balance').to.equal(
            0n,
        );

        // Non-cumulative: a later txless block shows a zero balance again, proving the
        // sweep (a cumulative account would keep growing).
        let emptyHeight: number | undefined;
        for (let i = 0; i < 12 && emptyHeight === undefined; i++) {
            const head = Number(await seiProvider.send('eth_blockNumber', []));
            for (let b = blockN + 1; b <= head; b++) {
                const blk = await seiProvider.send('eth_getBlockByNumber', [
                    '0x' + b.toString(16),
                    false,
                ]);
                if (blk.transactions.length === 0) {
                    emptyHeight = b;
                    break;
                }
            }
            if (emptyHeight === undefined) await new Promise(r => setTimeout(r, 1000));
        }
        expect(emptyHeight, 'expected a txless block after the tx to verify the sweep').to.not.equal(
            undefined,
        );
        const balEmpty = await bankBalanceUsei(feeCollectorAddr, emptyHeight!);
        expect(balEmpty, `fee_collector must be empty at the txless block ${emptyHeight}`).to.equal(
            0n,
        );
    });

    it('rejects extra parameters on Sei with -32602 and go-ethereum\'s exact message', async () => {
        // ethers strips extras client-side, so go raw. eth_coinbase takes no args, so
        // both a positional and an object argument must fail identically.
        const [positional, object] = await Promise.all([
            rawSei('eth_coinbase', ['latest']),
            rawSei('eth_coinbase', [{}]),
        ]);
        expectJsonRpcError(positional, -32602, /^too many arguments, want at most 0$/);
        expectJsonRpcError(object, -32602, /^too many arguments, want at most 0$/);
    });
});