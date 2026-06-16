import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount, deployedBytecodeOf, delegationDesignator, fundEvm, selfAuthorize, setCodeForEOA } from '../utils/evmUtils';
import { HEX_DATA } from '../utils/format';

// A Sei stateful precompile. It is callable but carries no EVM bytecode, so
// eth_getCode must report it as a codeless ("0x") account.
const STAKING_PRECOMPILE = '0x0000000000000000000000000000000000001005';
const ZERO_ADDRESS = '0x' + '0'.repeat(40);

const ERC20_RUNTIME = () => deployedBytecodeOf('TestERC20.sol', 'TestERC20');

describe('eth_getCode', function () {
    this.timeout(180 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let seiAdmin: string;
    let gethAdmin: string;
    let erc20Sei: string;
    let erc20Geth: string;

    before(() => {
        runtime = readRuntimeState();
        seiAdmin = runtime.funded.admin;
        gethAdmin = runtime.funded.gethAdmin.address;
        erc20Sei = runtime.contracts.erc20;
        erc20Geth = runtime.contracts.erc20Geth;
    });

    describe('happy path / schema', () => {
        it('returns 0x for a funded EOA (an externally owned account has no code)', async () => {
            const res = await rawSei<string>('eth_getCode', [seiAdmin, 'latest']);
            expect(res.error, JSON.stringify(res.error)).to.equal(undefined);
            expect(res.result, 'EOA has no code').to.equal('0x');
        });

        it('returns the exact runtime bytecode for a deployed contract', async () => {
            const code = await sei.send('eth_getCode', [erc20Sei, 'latest']);
            expect(code).to.match(HEX_DATA);
            expect(code).to.equal(ERC20_RUNTIME());
        });

        it('returns well-formed, non-empty code for every deployed contract type', async () => {
            const addrs = [
                runtime.contracts.erc20,
                runtime.contracts.simpleAccount7702,
                runtime.contracts.gasBurner,
            ];
            const codes = await Promise.all(
                addrs.map(a => sei.send('eth_getCode', [a, 'latest'])),
            );
            codes.forEach((code, i) => {
                expect(code, `${addrs[i]} is canonical hex data`).to.match(HEX_DATA);
                expect(code.length, `${addrs[i]} has bytecode`).to.be.greaterThan(2);
            });
        });

        it('returns 0x for the zero address', async () => {
            const code = await sei.send('eth_getCode', [ZERO_ADDRESS, 'latest']);
            expect(code).to.equal('0x');
        });
    });

    describe('geth compatibility (schema + byte-for-byte parity)', () => {
        it('the same contract source returns byte-identical code on Sei and geth', async () => {
            const [s, g] = await Promise.all([
                sei.send('eth_getCode', [erc20Sei, 'latest']),
                geth.send('eth_getCode', [erc20Geth, 'latest']),
            ]);
            expect(s, 'Sei code is canonical hex').to.match(HEX_DATA);
            expect(g, 'geth code is canonical hex').to.match(HEX_DATA);
            expect(s, 'identical artifact ⇒ identical runtime bytecode').to.equal(g);
        });

        it('an EOA returns 0x identically on Sei and geth', async () => {
            const [s, g] = await Promise.all([
                rawSei<string>('eth_getCode', [seiAdmin, 'latest']),
                rawGeth<string>('eth_getCode', [gethAdmin, 'latest']),
            ]);
            expect(s.result, 'Sei: EOA has no code').to.equal('0x');
            expect(g.result, 'geth: EOA has no code').to.equal('0x');
        });
    });

    describe('historical state', () => {
        it('reports no code before the deploy block and the full code at the deploy block', async () => {
            const [before, atDeploy] = await Promise.all([
                sei.send('eth_getCode', [
                    erc20Sei,
                    ethers.toQuantity(runtime.blocks.seiBeforeDeploy),
                ]),
                sei.send('eth_getCode', [erc20Sei, ethers.toQuantity(runtime.blocks.seiErc20Deploy)]),
            ]);
            expect(before, 'contract did not exist yet').to.equal('0x');
            expect(atDeploy, 'code present from the deploy block').to.equal(ERC20_RUNTIME());
        });

        it('agrees across latest / pending / safe / finalized for an immutable contract', async () => {
            const tags = ['latest', 'pending', 'safe', 'finalized'] as const;
            const codes = await Promise.all(
                tags.map(t => rawSei<string>('eth_getCode', [erc20Sei, t])),
            );
            codes.forEach((res, i) =>
                expect(res.error, `${tags[i]}: ${JSON.stringify(res.error)}`).to.equal(undefined),
            );
            const latest = codes[0].result;
            codes.forEach((res, i) =>
                expect(res.result, `${tags[i]} == latest (deployed code is immutable)`).to.equal(
                    latest,
                ),
            );
        });
    });

    describe('block specifiers (EIP-1898)', () => {
        it('a blockNumber object matches the numeric tag and the deployed code', async () => {
            const tag = ethers.toQuantity(runtime.blocks.seiErc20Deploy);
            const [viaTag, viaObject] = await Promise.all([
                rawSei<string>('eth_getCode', [erc20Sei, tag]),
                rawSei<string>('eth_getCode', [erc20Sei, { blockNumber: tag }]),
            ]);
            expect(viaObject.result, 'blockNumber object == numeric tag').to.equal(viaTag.result);
            expect(viaObject.result).to.equal(ERC20_RUNTIME());
        });

        it('a blockHash object matches the numeric tag', async () => {
            const block = await sei.getBlock(runtime.blocks.seiErc20Deploy);
            expect(block, 'deploy block exists').to.not.equal(null);
            const [viaNumber, viaHash] = await Promise.all([
                rawSei<string>('eth_getCode', [
                    erc20Sei,
                    ethers.toQuantity(runtime.blocks.seiErc20Deploy),
                ]),
                rawSei<string>('eth_getCode', [erc20Sei, { blockHash: block!.hash! }]),
            ]);
            expect(viaHash.result, 'blockHash object == numeric tag').to.equal(viaNumber.result);
            expect(viaHash.result).to.equal(ERC20_RUNTIME());
        });
    });

    describe('contract & account type coverage', () => {
        it('a precompile address has no EVM bytecode (0x)', async () => {
            const code = await sei.send('eth_getCode', [STAKING_PRECOMPILE, 'latest']);
            expect(code, 'stateful precompiles carry no EVM code').to.equal('0x');
        });

        it('a CW20 ERC20 pointer exposes non-empty pointer bytecode', async function () {
            if (!runtime.wasm?.cw20Pointer) {
                this.skip();
            }
            const code = await sei.send('eth_getCode', [runtime.wasm!.cw20Pointer, 'latest']);
            expect(code, 'pointer code is canonical hex data').to.match(HEX_DATA);
            expect(code.length, 'pointer is backed by real EVM bytecode').to.be.greaterThan(2);
        });
    });

    describe('EIP-7702 delegated accounts', () => {
        let funder: EvmAccount;

        before(() => {
            [funder] = claimPool(runtime, sei, 1, 'eth_getCode-7702');
        });

        const freshFunded = async (): Promise<EvmAccount> => {
            const acct = EvmAccount.random(sei);
            await fundEvm(funder, acct.address, ethers.parseEther('0.1'));
            return acct;
        };

        it('returns the designator when SimpleAccount7702 is set as the EOA authentication target', async () => {
            // The designator (0xef0100 || impl) lives on the *EOA* that authorized the
            // delegation — not on the implementation contract, which keeps its own code.
            const acct = await freshFunded();
            const impl = runtime.contracts.simpleAccount7702;

            const receipt = await setCodeForEOA(acct, [await selfAuthorize(acct, impl)]);
            expect(receipt!.status, 'set-code tx succeeded').to.equal(1);

            const code = await sei.send('eth_getCode', [acct.address, 'latest']);
            expect(code.toLowerCase()).to.equal(delegationDesignator(impl));
        });

        it('reports the 0xef0100 delegation designator after a set-code (type-4) tx', async () => {
            const acct = await freshFunded();
            const impl = runtime.contracts.simpleAccount7702;

            const preBlock = await sei.getBlockNumber();
            expect(await sei.send('eth_getCode', [acct.address, 'latest']), 'clean EOA').to.equal(
                '0x',
            );

            const receipt = await setCodeForEOA(acct, [await selfAuthorize(acct, impl)]);
            expect(receipt!.status, 'set-code tx succeeded').to.equal(1);

            const code = await sei.send('eth_getCode', [acct.address, 'latest']);
            expect(code.toLowerCase(), 'code is the delegation designator').to.equal(
                delegationDesignator(impl),
            );

            // The delegation must not be back-dated onto the pre-delegation block.
            const before = await sei.send('eth_getCode', [
                acct.address,
                ethers.toQuantity(preBlock),
            ]);
            expect(before, 'no code before the account was delegated').to.equal('0x');
        });

        it('re-delegating to a new target replaces the reported code (modification differs from the original)', async () => {
            const acct = await freshFunded();
            const implA = runtime.contracts.simpleAccount7702;
            const implB = runtime.contracts.erc20;

            await setCodeForEOA(acct, [await selfAuthorize(acct, implA)]);
            const original = await sei.send('eth_getCode', [acct.address, 'latest']);
            expect(original.toLowerCase()).to.equal(delegationDesignator(implA));

            // Re-delegate to implB. The type-4 tx targets a throwaway address (not the
            // account itself), so implB's code is never executed — only the new
            // authorization is applied, swapping the stored delegation designator.
            const net = await sei.getNetwork();
            const fee = await sei.getFeeData();
            const nonce = await acct.nonce('latest');
            const authB = await acct.wallet.authorize({
                address: implB,
                chainId: net.chainId,
                nonce: nonce + 1,
            });
            const tx = await acct.wallet.sendTransaction({
                to: ethers.Wallet.createRandom().address,
                value: 0,
                type: 4,
                authorizationList: [authB],
                maxFeePerGas: fee.maxFeePerGas!,
                maxPriorityFeePerGas: fee.maxPriorityFeePerGas!,
            });
            const receipt = await tx.wait();
            expect(receipt!.status, 're-delegation tx succeeded').to.equal(1);

            const modified = await sei.send('eth_getCode', [acct.address, 'latest']);
            expect(modified.toLowerCase(), 'code now points at impl B').to.equal(
                delegationDesignator(implB),
            );
            expect(modified, 'modified code differs from the original delegation').to.not.equal(
                original,
            );
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getCode', []),
                rawGeth('eth_getCode', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('omitting the block argument fails identically (-32602, missing required argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getCode', [seiAdmin]),
                rawGeth('eth_getCode', [gethAdmin]),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 1/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 2)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getCode', [seiAdmin, 'latest', {}]),
                rawGeth('eth_getCode', [gethAdmin, 'latest', {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 2/);
            expectSameError(s, g);
        });

        it('non-array params fail identically (-32602, non-array args)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getCode', { address: seiAdmin }),
                rawGeth('eth_getCode', { address: gethAdmin }),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });

        it('a malformed (too short) address fails identically (-32602, exact length message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getCode', ['0x1234', 'latest']),
                rawGeth('eth_getCode', ['0x1234', 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 4, want 40 for common\.Address/);
            expectSameError(s, g);
        });
    });
});
