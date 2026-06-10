import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import {
    readRuntimeState,
    RuntimeState,
    claimPool as claimFromPool,
    expectSameError,
} from '../utils/testUtils';
import { abiOf, bytecodeOf, EvmAccount, authToRpc } from '../utils/evmUtils';
import { burnGasBurst } from '../utils/txUtils';
import { HEX_QUANTITY } from '../utils/format';
import { STAKING_PRECOMPILE_ADDRESS } from '../utils/constants';

describe('eth_estimateGas Tests', function () {
    this.timeout(180 * 1000);

    const { sei, geth } = bothProviders();
    const erc20Iface = new ethers.Interface(abiOf('TestERC20.sol', 'TestERC20'));
    const burnerIface = new ethers.Interface(abiOf('GasBurner.sol', 'RealGasBurner'));
    const BOB = '0x000000000000000000000000000000000000bEEF';
    const INTRINSIC = 21000n;

    let runtime: RuntimeState;
    let erc20Sei: string;
    let erc20Geth: string;
    let seiAdmin: string;
    let gethAdmin: string;
    let gasBurner: string;
    let simpleAccountAddress: string;

    let actor: EvmAccount;
    let spammers: EvmAccount[];

    const transferData = (to: string, amount: bigint): string =>
        erc20Iface.encodeFunctionData('transfer', [to, amount]);
    const validatorsData = (): string =>
        new ethers.Interface([
            'function validators(string status, bytes pagination) returns (bytes,bytes)',
        ]).encodeFunctionData('validators', ['BOND_STATUS_BONDED', '0x']);

    const estimate = async (
        provider: ethers.JsonRpcProvider,
        tx: Record<string, unknown>,
        block?: string,
    ): Promise<bigint> =>
        BigInt(await provider.send('eth_estimateGas', block ? [tx, block] : [tx]));

    const claimPool = (count: number, salt: string): EvmAccount[] =>
        claimFromPool(runtime, sei, count, salt);

    before(async () => {
        runtime = readRuntimeState();
        erc20Sei = runtime.contracts.erc20;
        erc20Geth = runtime.contracts.erc20Geth;
        seiAdmin = runtime.funded.admin;
        gethAdmin = runtime.funded.gethAdmin.address;
        gasBurner = runtime.contracts.gasBurner;
        simpleAccountAddress = runtime.contracts.simpleAccount7702;

        const pool = claimPool(6, 'eth_estimateGas');
        actor = pool[0];
        spammers = pool.slice(1);
    });

    describe('eth_estimateGas Queries', () => {
        it('a bare native transfer costs exactly the intrinsic 21000', async () => {
            expect(await estimate(sei, { from: seiAdmin, to: BOB, value: '0x1' })).to.equal(INTRINSIC);
        });

        it('a zero-value transfer still costs the intrinsic 21000', async () => {
            expect(await estimate(sei, { from: seiAdmin, to: BOB, value: '0x0' })).to.equal(INTRINSIC);
        });

        it('a self-transfer costs the intrinsic 21000', async () => {
            expect(await estimate(sei, { from: seiAdmin, to: seiAdmin, value: '0x1' })).to.equal(
                INTRINSIC,
            );
        });

        it('calldata on a plain transfer raises the estimate above the intrinsic', async () => {
            const withData = await estimate(sei, { from: seiAdmin, to: BOB, data: '0x1234567890' });
            expect(withData > INTRINSIC).to.equal(true);
        });

        it('an ERC20 approve and mint both estimate above the intrinsic', async () => {
            const [approveEst, mintEst] = await Promise.all([
                estimate(sei, {
                    from: seiAdmin,
                    to: erc20Sei,
                    data: erc20Iface.encodeFunctionData('approve', [BOB, ethers.parseEther('100')]),
                }),
                estimate(sei, {
                    from: seiAdmin,
                    to: erc20Sei,
                    data: erc20Iface.encodeFunctionData('mint', [seiAdmin, ethers.parseEther('100')]),
                }),
            ]);
            expect(approveEst > INTRINSIC).to.equal(true);
            expect(mintEst > INTRINSIC).to.equal(true);
        });

        it('a contract deployment estimates into the hundreds of thousands of gas', async () => {
            const deployData =
                bytecodeOf('TestERC20.sol', 'TestERC20') +
                ethers.AbiCoder.defaultAbiCoder().encode(['address'], [seiAdmin]).slice(2);
            const est = await estimate(sei, { data: deployData });
            expect(est > 500_000n).to.equal(true);
        });

        it('estimate gas calls accepts an explicit latest block tag', async () => {
            const est = await estimate(sei, { from: seiAdmin, to: BOB, value: '0x1' }, 'latest');
            expect(est).to.equal(INTRINSIC);
        });

        it('estimating against pending agrees with latest for a stable call', async () => {
            const [atLatest, atPending] = await Promise.all([
                estimate(sei, { from: seiAdmin, to: erc20Sei, data: transferData(BOB, 1n) }, 'latest'),
                estimate(sei, { from: seiAdmin, to: erc20Sei, data: transferData(BOB, 1n) }, 'pending'),
            ]);
            expect(atPending).to.equal(atLatest);
        });
    });

    describe('transaction types', () => {
        const base = () => ({ from: seiAdmin, to: BOB, value: '0x1' });

        it('legacy (type 0) and EIP-1559 (type 2) estimate the same units as a bare transfer', async () => {
            const [legacy, eip1559] = await Promise.all([
                estimate(sei, { ...base(), type: '0x0' }),
                estimate(sei, { ...base(), type: '0x2' }),
            ]);
            expect(legacy, 'type 0').to.equal(INTRINSIC);
            expect(eip1559, 'type 2').to.equal(INTRINSIC);
        });

        it('an access-list (type 1) tx adds the EIP-2930 surcharge', async () => {
            const accessList = [
                { address: erc20Sei, storageKeys: ['0x' + '00'.repeat(32)] },
            ];
            const withAccessList = await estimate(sei, { ...base(), type: '0x1', accessList });
            // 2400 per address + 1900 per storage key on top of the intrinsic transfer.
            expect(withAccessList - INTRINSIC >= 2400n + 1900n).to.equal(true);
        });

        it('a set-code (type 4) tx adds the per-authorization cost', async () => {
            const authority = ethers.Wallet.createRandom();
            const auth = await authority.authorize({
                address: simpleAccountAddress,
                chainId: 0,
                nonce: 0,
            });
            const est = await estimate(sei, {
                from: seiAdmin,
                to: seiAdmin,
                type: '0x4',
                authorizationList: [authToRpc(auth)],
            });
            // A fresh authority is an empty account: PER_EMPTY_ACCOUNT_COST is 25000.
            expect(est - INTRINSIC >= 25_000n).to.equal(true);
        });
    });

    describe('estimate accuracy', () => {
        it('the ERC20 transfer estimate bounds and closely tracks real gas used', async () => {
            const erc20 = new ethers.Contract(erc20Sei, erc20Iface, actor.wallet);
            await (await erc20.mint(actor.address, ethers.parseEther('100'))).wait();

            const recipient = ethers.Wallet.createRandom().address;
            const data = transferData(recipient, ethers.parseEther('1'));
            const est = await estimate(sei, { from: actor.address, to: erc20Sei, data });

            const tx = await actor.wallet.sendTransaction({ to: erc20Sei, data, gasLimit: est });
            const receipt = await tx.wait();
            expect(receipt!.status).to.equal(1);
            expect(receipt!.gasUsed <= est, 'estimate must bound actual usage').to.equal(true);

            const overshootPct = Number((est - receipt!.gasUsed) * 100n) / Number(est);
            expect(overshootPct, 'estimate should be close to actual').to.be.lessThan(10);
        });

        it('a native transfer estimate equals its exact gas used', async () => {
            const est = await estimate(sei, { from: actor.address, to: BOB, value: '0x1' });
            const tx = await actor.wallet.sendTransaction({ to: BOB, value: 1n, gasLimit: est });
            const receipt = await tx.wait();
            expect(receipt!.gasUsed).to.equal(INTRINSIC);
            expect(est).to.equal(INTRINSIC);
        });

        it('repeated estimates of the same call are deterministic', async () => {
            const tx = { from: seiAdmin, to: erc20Sei, data: transferData(BOB, 1n) };
            const results = await Promise.all(Array.from({ length: 5 }, () => estimate(sei, tx)));
            results.forEach(r => expect(r).to.equal(results[0]));
        });

        it('a generous gas cap does not change the estimate', async () => {
            const tx = { from: seiAdmin, to: erc20Sei, data: transferData(BOB, 1n) };
            const [withCap, withoutCap] = await Promise.all([
                estimate(sei, { ...tx, gas: '0x4c4b40' }),
                estimate(sei, tx),
            ]);
            expect(withCap).to.equal(withoutCap);
        });
    });

    describe('precompiles', () => {
        it('estimates the staking validators() precompile call above the intrinsic', async () => {
            const est = await estimate(sei, {
                from: seiAdmin,
                to: STAKING_PRECOMPILE_ADDRESS,
                data: validatorsData(),
            });
            expect(est > INTRINSIC).to.equal(true);
        });

        it('the precompile estimate is deterministic and bounds real execution', async () => {
            const data = validatorsData();
            const est = await estimate(sei, {
                from: actor.address,
                to: STAKING_PRECOMPILE_ADDRESS,
                data,
            });
            const again = await estimate(sei, {
                from: actor.address,
                to: STAKING_PRECOMPILE_ADDRESS,
                data,
            });
            expect(again).to.equal(est);

            const tx = await actor.wallet.sendTransaction({
                to: STAKING_PRECOMPILE_ADDRESS,
                data,
                gasLimit: est,
            });
            const receipt = await tx.wait();
            expect(receipt!.status).to.equal(1);
            expect(receipt!.gasUsed <= est, 'estimate must bound actual precompile usage').to.equal(
                true,
            );
        });
    });

    describe('schema matching vs geth', () => {
        it('a native transfer estimates to 21000 on both Sei and geth', async () => {
            const [s, g] = await Promise.all([
                estimate(sei, { from: seiAdmin, to: BOB, value: '0x1' }),
                estimate(geth, { from: gethAdmin, to: BOB, value: '0x1' }),
            ]);
            expect(s).to.equal(INTRINSIC);
            expect(g).to.equal(INTRINSIC);
        });

        it('an ERC20 transfer estimates identically on Sei and geth', async () => {
            // A never-seen recipient is a cold (new) balance slot on both chains, so the
            // estimate is the full-write cost and matches byte-for-byte.
            const data = transferData(ethers.Wallet.createRandom().address, ethers.parseEther('1'));
            const [s, g] = await Promise.all([
                estimate(sei, { from: seiAdmin, to: erc20Sei, data }),
                estimate(geth, { from: gethAdmin, to: erc20Geth, data }),
            ]);
            expect(s).to.equal(g);
        });

        it('access-list, EIP-1559 and set-code estimates all match geth', async () => {
            const accessList = [{ address: '0x' + '11'.repeat(20), storageKeys: ['0x' + '00'.repeat(32)] }];
            const authority = ethers.Wallet.createRandom();
            const auth = authToRpc(
                await authority.authorize({ address: '0x' + '22'.repeat(20), chainId: 0, nonce: 0 }),
            );

            const variants: Record<string, unknown>[] = [
                { value: '0x1', type: '0x2' },
                { value: '0x1', type: '0x1', accessList },
                { to: undefined, type: '0x4', authorizationList: [auth] },
            ];
            for (const v of variants) {
                const { to: vTo, ...rest } = v as { to?: string };
                const [s, g] = await Promise.all([
                    estimate(sei, { from: seiAdmin, to: vTo ?? seiAdmin, ...rest }),
                    estimate(geth, { from: gethAdmin, to: vTo ?? gethAdmin, ...rest }),
                ]);
                expect(s, `variant ${JSON.stringify(v)}`).to.equal(g);
            }
        });

        it('an identical contract deployment estimates the same on Sei and geth', async () => {
            const deployData =
                bytecodeOf('TestERC20.sol', 'TestERC20') +
                ethers.AbiCoder.defaultAbiCoder().encode(['address'], [seiAdmin]).slice(2);
            const [s, g] = await Promise.all([
                estimate(sei, { data: deployData }),
                estimate(geth, { data: deployData }),
            ]);
            expect(s).to.equal(g);
        });
    });

    describe('empty / null handling', () => {
        it('returns a canonical quantity, never null (raw transport)', async () => {
            const body = await rawSei<string>('eth_estimateGas', [
                { from: seiAdmin, to: BOB, value: '0x1' },
            ]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result).to.match(HEX_QUANTITY);
            expect(body.result).to.not.equal(null);
        });

        it('a contract call returns a quantity, never null (raw transport)', async () => {
            const body = await rawSei<string>('eth_estimateGas', [
                { from: seiAdmin, to: erc20Sei, data: transferData(BOB, 1n) },
            ]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result).to.match(HEX_QUANTITY);
        });
    });

    describe('wrong params / error handling', () => {
        it('a revert surfaces with identical code 3, message and decodable revert data on both', async () => {
            const sender = ethers.Wallet.createRandom().address;
            const data = transferData(BOB, ethers.parseEther('1000000000'));
            const [s, g] = await Promise.all([
                rawSei('eth_estimateGas', [{ from: sender, to: erc20Sei, data }]),
                rawGeth('eth_estimateGas', [{ from: sender, to: erc20Geth, data }]),
            ]);
            const err = expectJsonRpcError(s, 3, /execution reverted/i);
            expect(err.data).to.be.a('string');
            const reason = ethers.AbiCoder.defaultAbiCoder().decode(
                ['string'],
                '0x' + (err.data as string).slice(10),
            )[0];
            expect(reason, 'revert reason decodes from error data').to.equal(
                'ERC20: insufficient balance',
            );
            expectSameError(s, g);
        });

        it('a bad selector reverts with code 3 and empty data on both', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_estimateGas', [{ from: seiAdmin, to: erc20Sei, data: '0x12345678' }]),
                rawGeth('eth_estimateGas', [{ from: gethAdmin, to: erc20Geth, data: '0x12345678' }]),
            ]);
            expectJsonRpcError(s, 3, /execution reverted/i);
            expect(s.error!.data).to.equal('0x');
            expectSameError(s, g);
        });

        it('a gas cap below the requirement fails identically to geth (-32000 allowance)', async () => {
            const data = transferData(seiAdmin, ethers.parseEther('1'));
            const [s, g] = await Promise.all([
                rawSei('eth_estimateGas', [{ from: seiAdmin, to: erc20Sei, data, gas: '0x5208' }]),
                rawGeth('eth_estimateGas', [{ from: gethAdmin, to: erc20Geth, data, gas: '0x5208' }]),
            ]);
            expectJsonRpcError(s, -32000, /gas required exceeds allowance/);
            expectSameError(s, g);
        });

        it('a malformed from address fails identically to geth (-32602, exact message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_estimateGas', [{ from: '0xdead', to: BOB, value: '0x1' }]),
                rawGeth('eth_estimateGas', [{ from: '0xdead', to: BOB, value: '0x1' }]),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 4, want 40 for common\.Address/);
            expectSameError(s, g);
        });

        it('empty params fail identically to geth (-32602 missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_estimateGas', []),
                rawGeth('eth_estimateGas', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('[divergence] insufficient funds: both -32000 with the same clause, different gas prefix', async () => {
            const value = ethers.toQuantity(ethers.parseEther('1000000'));
            const [s, g] = await Promise.all([
                rawSei('eth_estimateGas', [{ from: BOB, to: seiAdmin, value }]),
                rawGeth('eth_estimateGas', [{ from: BOB, to: gethAdmin, value }]),
            ]);
            expect(s.error?.code, 'sei code').to.equal(-32000);
            expect(g.error?.code, 'geth code').to.equal(-32000);
            expect(s.error?.message).to.match(/insufficient funds for gas \* price \+ value/);
            expect(g.error?.message).to.match(/insufficient funds for gas \* price \+ value/);
            // The "failed with N gas" prefix encodes each node's block gas cap, which differs.
            expect(s.error?.message).to.not.equal(g.error?.message);
        });

        it('[divergence] far-future block: both -32000 but different messages', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_estimateGas', [{ from: seiAdmin, to: BOB, value: '0x1' }, '0xffffffff']),
                rawGeth('eth_estimateGas', [{ from: gethAdmin, to: BOB, value: '0x1' }, '0xffffffff']),
            ]);
            expect(s.error?.code, 'sei code').to.equal(-32000);
            expect(g.error?.code, 'geth code').to.equal(-32000);
            expect(s.error?.message).to.match(/not yet available/i);
            expect(g.error?.message).to.match(/header not found/i);
            expect(s.error?.message).to.not.equal(g.error?.message);
        });
    });

    describe('base fee increase doesnt change the gas estimates', () => {
        const getBaseFee = async (): Promise<bigint> => {
            const blk = await sei.send('eth_getBlockByNumber', ['latest', false]);
            return BigInt(blk.baseFeePerGas ?? '0x0');
        };

        it('gas estimates stay correct (and bound real usage) as the base fee rises', async function () {
            const burnData = burnerIface.encodeFunctionData('burnGasIterations', [7n, 30n]);
            const estimateBefore = await estimate(sei, { from: actor.address, to: gasBurner, data: burnData });

            const { beforeBaseFee: before } = await burnGasBurst(sei, runtime, spammers);
            const after = await getBaseFee();
            expect(after > before, 'base fee should have risen').to.equal(true);

            // Gas is denominated in units; a fee-market move must not change the estimate.
            const estimateAfter = await estimate(sei, { from: actor.address, to: gasBurner, data: burnData });
            expect(estimateAfter, 'gas units are independent of the base fee').to.equal(estimateBefore);

            const fee = await sei.getFeeData();
            const tx = await actor.wallet.sendTransaction({
                to: gasBurner,
                data: burnData,
                gasLimit: estimateAfter,
                maxFeePerGas: fee.maxFeePerGas!,
                maxPriorityFeePerGas: fee.maxPriorityFeePerGas!,
            });
            const receipt = await tx.wait();
            expect(receipt!.status).to.equal(1);
            expect(receipt!.gasUsed <= estimateAfter, 'estimate must still bound usage').to.equal(true);
        });
    });
});
