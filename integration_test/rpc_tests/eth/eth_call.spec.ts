import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders } from '../utils/chainUtils';
import { rawSei, rawGeth, captureRpcError, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';
import { abiOf } from '../utils/evmUtils';
import { EvmAccount } from '../utils/evmUtils';
import { HEX_DATA } from '../utils/format';
import { SIMPLE_ACCOUNT_ABI, delegationDesignator, selfAuthorize, setCodeForEOA } from '../utils/evmUtils';
import { Erc20Calldata, claimPool, encodeUint, expectSameError } from '../utils/testUtils';
import { STAKING_PRECOMPILE_ADDRESS } from '../utils/constants';

describe('eth_call', function () {
    this.timeout(120 * 1000);

    const { sei, geth } = bothProviders();
    const erc20Iface = new ethers.Interface(abiOf('TestERC20.sol', 'TestERC20'));
    const erc20 = new Erc20Calldata(erc20Iface);
    const ADMIN_MINT = ethers.parseEther('1000000');

    let runtime: RuntimeState;
    let erc20Sei: string;
    let erc20Geth: string;
    let seiAdmin: string;
    let gethAdmin: string;

    // Claimed from the pre-funded pool to avoid serialising on the shared admin nonce.
    let minter: EvmAccount;
    let alice: EvmAccount;
    let aliceRevert: EvmAccount;
    let simpleAccountAddress: string;

    before(async function () {
        runtime = readRuntimeState();
        erc20Sei = runtime.contracts.erc20;
        erc20Geth = runtime.contracts.erc20Geth;
        seiAdmin = runtime.funded.admin;
        gethAdmin = runtime.funded.gethAdmin.address;

        [minter, alice, aliceRevert] = claimPool(runtime, sei, 3, 'eth_call');
        simpleAccountAddress = runtime.contracts.simpleAccount7702;
    });

    describe('happy path', () => {
        it('balanceOf returns the expected balance at latest', async () => {
            const result = await sei.send('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                'latest',
            ]);
            expect(erc20.decodeBalance(result)).to.equal(ADMIN_MINT);
        });

        it('omitting the block tag defaults to latest', async () => {
            const [withoutTag, withLatest] = await Promise.all([
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }]),
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, 'latest']),
            ]);
            expect(withoutTag).to.equal(withLatest);
        });

        it('a call against an EOA (no code) returns 0x', async () => {
            const result = await sei.send('eth_call', [
                { to: seiAdmin, data: '0x12345678' },
                'latest',
            ]);
            expect(result).to.equal('0x');
        });

        it('simulating an ERC20 transfer does not change state', async () => {
            const before = erc20.decodeBalance(
                await sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, 'latest']),
            );
            const simulated = await sei.send('eth_call', [
                { from: seiAdmin, to: erc20Sei, data: erc20.transfer(seiAdmin, ethers.parseEther('1')) },
                'latest',
            ]);
            expect(erc20Iface.decodeFunctionResult('transfer', simulated)[0]).to.equal(true);

            const after = erc20.decodeBalance(
                await sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, 'latest']),
            );
            expect(after).to.equal(before);
        });

        it('a call at a block before the contract was deployed returns 0x', async () => {
            const result = await sei.send('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                ethers.toQuantity(runtime.blocks.seiBeforeDeploy),
            ]);
            expect(result).to.equal('0x');
        });

        it('respects historical state across distinct mint blocks and does not leak latest state', async () => {
            const recipient = ethers.Wallet.createRandom().address;
            const FIRST_MINT = ethers.parseEther('100');
            const SECOND_MINT = ethers.parseEther('50');

            const blockBeforeFirstMint = await sei.getBlockNumber();
            const erc20Contract = new ethers.Contract(erc20Sei, erc20Iface, minter.wallet);

            const firstReceipt = await (await erc20Contract.mint(recipient, FIRST_MINT)).wait();
            const blockOfFirstMint = firstReceipt!.blockNumber;
            const secondReceipt = await (await erc20Contract.mint(recipient, SECOND_MINT)).wait();
            const blockOfSecondMint = secondReceipt!.blockNumber;

            expect(blockOfSecondMint).to.be.greaterThan(
                blockOfFirstMint,
                'second mint must land in a strictly later block',
            );

            const balanceAt = async (block: number): Promise<bigint> =>
                erc20.decodeBalance(
                    await sei.send('eth_call', [
                        { to: erc20Sei, data: erc20.balanceOf(recipient) },
                        ethers.toQuantity(block),
                    ]),
                );

            expect(await balanceAt(blockBeforeFirstMint)).to.equal(0n);
            expect(await balanceAt(blockOfFirstMint)).to.equal(FIRST_MINT);
            expect(await balanceAt(blockOfSecondMint - 1)).to.equal(FIRST_MINT);
            expect(await balanceAt(blockOfSecondMint)).to.equal(FIRST_MINT + SECOND_MINT);
        });

        it('accepts an EIP-1898 blockNumber object equivalently to a tag', async () => {
            const latest = await sei.getBlockNumber();
            const [viaTag, viaObject] = await Promise.all([
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, ethers.toQuantity(latest)]),
                sei.send('eth_call', [
                    { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                    { blockNumber: ethers.toQuantity(latest) },
                ]),
            ]);
            expect(viaObject).to.equal(viaTag);
        });

        it('accepts an EIP-1898 blockHash object equivalently to a tag', async () => {
            const latest = await sei.getBlock('latest');
            expect(latest, 'latest block should exist').to.not.equal(null);
            const [viaTag, viaObject] = await Promise.all([
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, ethers.toQuantity(latest!.number)]),
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, { blockHash: latest!.hash! }]),
            ]);
            expect(viaObject).to.equal(viaTag);
        });

        it('returns the same result across latest, pending, safe and finalized tags', async () => {
            const latest = await sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, 'latest']);
            for (const tag of ['pending', 'safe', 'finalized']) {
                const result = await sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, tag]);
                expect(result, `tag ${tag} must equal latest`).to.equal(latest);
            }
        });

        it('Honours a state override that replaces contract bytecode', async () => {
            const stub = '0x7f00000000000000000000000000000000000000000000000000000000deadbeef60005260206000f3';
            const result = await sei.send('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                'latest',
                { [erc20Sei.toLowerCase()]: { code: stub } },
            ]);
            expect(result).to.equal(
                '0x00000000000000000000000000000000000000000000000000000000deadbeef',
            );
        });

        it('[Sei-specific] the staking validators() precompile decodes to a complete ValidatorsResponse', async () => {
            // ValidatorsResponse { Validator[] validators; bytes nextKey; }
            // see sei-chain/precompiles/staking/Staking.sol.
            const iface = new ethers.Interface([
                'function validators(string status, bytes nextKey) view returns (' +
                    'tuple(tuple(string operatorAddress, bytes consensusPubkey, bool jailed, int32 status, ' +
                    'string tokens, string delegatorShares, string description, int64 unbondingHeight, ' +
                    'int64 unbondingTime, string commissionRate, string commissionMaxRate, ' +
                    'string commissionMaxChangeRate, int64 commissionUpdateTime, string minSelfDelegation)[] ' +
                    'validators, bytes nextKey) response)',
            ]);
            const data = iface.encodeFunctionData('validators', ['BOND_STATUS_BONDED', '0x']);
            const result = await sei.send('eth_call', [{ to: STAKING_PRECOMPILE_ADDRESS, data }, 'latest']);
            expect(result).to.match(HEX_DATA);

            const [response] = iface.decodeFunctionResult('validators', result);
            const validators = response.validators as ReadonlyArray<{
                operatorAddress: string;
                consensusPubkey: string;
                jailed: boolean;
                status: bigint;
                tokens: string;
                commissionRate: string;
            }>;

            expect(validators.length, 'the devnet exposes bonded validators').to.be.greaterThan(0);
            expect(response.nextKey, 'a single page covers all validators').to.equal('0x');

            for (const v of validators) {
                expect(v.operatorAddress, 'bech32 valoper address').to.match(/^seivaloper1[0-9a-z]{38}$/);
                expect(Number(v.status), 'BOND_STATUS_BONDED == 3').to.equal(3);
                expect(v.jailed, 'a bonded validator is not jailed').to.equal(false);
                expect(BigInt(v.tokens) > 0n, `staked tokens positive (got ${v.tokens})`).to.equal(true);
                expect(v.consensusPubkey, 'consensus pubkey present').to.match(HEX_DATA);
                expect(v.consensusPubkey.length, 'consensus pubkey non-empty').to.be.greaterThan(2);
                expect(v.commissionRate, 'commission rate present').to.match(/^\d+\.\d+$/);
            }
        });
    });

    describe('schema matching', () => {
        it('balanceOf returns identical 32-byte data on Sei and geth (same minted amount)', async () => {
            const [seiResult, gethResult] = await Promise.all([
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, 'latest']),
                geth.send('eth_call', [{ to: erc20Geth, data: erc20.balanceOf(gethAdmin) }, 'latest']),
            ]);
            expect(seiResult, 'sei').to.match(HEX_DATA);
            expect(gethResult, 'geth').to.match(HEX_DATA);
            expect(erc20.decodeBalance(seiResult)).to.equal(ADMIN_MINT);
            expect(erc20.decodeBalance(gethResult)).to.equal(ADMIN_MINT);
            expect(seiResult).to.equal(gethResult);
        });

        it('a call against an EOA returns 0x on both Sei and geth', async () => {
            const [seiResult, gethResult] = await Promise.all([
                sei.send('eth_call', [{ to: seiAdmin, data: '0x12345678' }, 'latest']),
                geth.send('eth_call', [{ to: gethAdmin, data: '0x12345678' }, 'latest']),
            ]);
            expect(seiResult).to.equal('0x');
            expect(gethResult).to.equal('0x');
        });

        it('a call before the contract existed returns 0x on both Sei and geth', async () => {
            const [seiResult, gethResult] = await Promise.all([
                sei.send('eth_call', [
                    { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                    ethers.toQuantity(runtime.blocks.seiBeforeDeploy),
                ]),
                geth.send('eth_call', [
                    { to: erc20Geth, data: erc20.balanceOf(gethAdmin) },
                    ethers.toQuantity(runtime.blocks.ethErc20Deploy - 1),
                ]),
            ]);
            expect(seiResult).to.equal('0x');
            expect(gethResult).to.equal('0x');
        });

        it('historical state transitions are byte-identical across the contract lifecycle on Sei and geth', async () => {
            // before deploy ("0x") → deploy block, pre-mint (zero word) → post-mint (ADMIN_MINT).
            const [seiPhases, gethPhases] = await Promise.all([
                Promise.all([
                    sei.send('eth_call', [
                        { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                        ethers.toQuantity(runtime.blocks.seiBeforeDeploy),
                    ]),
                    sei.send('eth_call', [
                        { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                        ethers.toQuantity(runtime.blocks.seiErc20Deploy),
                    ]),
                    sei.send('eth_call', [
                        { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                        ethers.toQuantity(runtime.blocks.seiAfterDeploy),
                    ]),
                ]),
                Promise.all([
                    geth.send('eth_call', [
                        { to: erc20Geth, data: erc20.balanceOf(gethAdmin) },
                        ethers.toQuantity(runtime.blocks.ethErc20Deploy - 1),
                    ]),
                    geth.send('eth_call', [
                        { to: erc20Geth, data: erc20.balanceOf(gethAdmin) },
                        ethers.toQuantity(runtime.blocks.ethErc20Deploy),
                    ]),
                    geth.send('eth_call', [{ to: erc20Geth, data: erc20.balanceOf(gethAdmin) }, 'latest']),
                ]),
            ]);

            const expected = ['0x', encodeUint(0n), encodeUint(ADMIN_MINT)];
            expect(seiPhases, 'sei lifecycle').to.deep.equal(expected);
            expect(gethPhases, 'geth lifecycle').to.deep.equal(expected);
            expect(seiPhases, 'lifecycle parity').to.deep.equal(gethPhases);
        });

        it('a state override replacing bytecode yields identical output on Sei and geth', async () => {
            const stub = '0x7f00000000000000000000000000000000000000000000000000000000deadbeef60005260206000f3';
            const [seiResult, gethResult] = await Promise.all([
                sei.send('eth_call', [
                    { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                    'latest',
                    { [erc20Sei.toLowerCase()]: { code: stub } },
                ]),
                geth.send('eth_call', [
                    { to: erc20Geth, data: erc20.balanceOf(gethAdmin) },
                    'latest',
                    { [erc20Geth.toLowerCase()]: { code: stub } },
                ]),
            ]);
            expect(seiResult).to.equal(gethResult);
            expect(seiResult).to.equal(
                '0x00000000000000000000000000000000000000000000000000000000deadbeef',
            );
        });
    });

    describe('empty / null handling', () => {
        it('a void call returns the canonical "0x", not null (raw transport)', async () => {
            const body = await rawSei<string>('eth_call', [{ to: seiAdmin, data: '0x' }, 'latest']);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result).to.equal('0x');
            expect(body.result).to.not.equal(null);
        });

        it('a successful read never returns null (raw transport)', async () => {
            const body = await rawSei<string>('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                'latest',
            ]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result).to.be.a('string');
            expect(body.result).to.match(HEX_DATA);
        });
    });

    describe('wrong params / error handling', () => {
        it('rejects an invalid block tag identically to geth (-32602, exact message)', async () => {
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data }, 'banana']),
                rawGeth('eth_call', [{ to: erc20Geth, data }, 'banana']),
            ]);
            expectJsonRpcError(s, -32602, /hex string without 0x prefix/);
            expectSameError(s, g);
        });

        it('rejects a malformed from address identically to geth (-32602, exact message)', async () => {
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ from: '0xdeadbeef', to: erc20Sei, data }, 'latest']),
                rawGeth('eth_call', [{ from: '0xdeadbeef', to: erc20Geth, data }, 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 8, want 40 for common\.Address/);
            expectSameError(s, g);
        });

        it('rejects an odd-length to address identically to geth (-32602, exact message)', async () => {
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: '0x123', data }, 'latest']),
                rawGeth('eth_call', [{ to: '0x123', data }, 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /unmarshal hex string of odd length .*TransactionArgs\.to/);
            expectSameError(s, g);
        });

        it('rejects non-hex data identically to geth (-32602, exact message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data: 'notHex' }, 'latest']),
                rawGeth('eth_call', [{ to: erc20Geth, data: 'notHex' }, 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /without 0x prefix .*TransactionArgs\.data of type hexutil\.Bytes/);
            expectSameError(s, g);
        });

        it('rejects odd-length data identically to geth (-32602, exact message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data: '0x123' }, 'latest']),
                rawGeth('eth_call', [{ to: erc20Geth, data: '0x123' }, 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /odd length .*TransactionArgs\.data of type hexutil\.Bytes/);
            expectSameError(s, g);
        });

        it('rejects a non-hex gas value identically to geth (-32602, exact message)', async () => {
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data, gas: '-0x1' }, 'latest']),
                rawGeth('eth_call', [{ to: erc20Geth, data, gas: '-0x1' }, 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /TransactionArgs\.gas of type hexutil\.Uint64/);
            expectSameError(s, g);
        });

        it('rejects non-array params identically to geth (-32602 "non-array args")', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_call', { to: erc20Sei, data: erc20.balanceOf(seiAdmin) }),
                rawGeth('eth_call', { to: erc20Geth, data: erc20.balanceOf(gethAdmin) }),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });

        it('rejects empty params identically to geth (-32602 missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_call', []),
                rawGeth('eth_call', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('rejects too many positional args identically to geth (-32602 "want at most 4")', async () => {
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data }, 'latest', {}, {}, {}]),
                rawGeth('eth_call', [{ to: erc20Geth, data }, 'latest', {}, {}, {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 4/);
            expectSameError(s, g);
        });

        it('rejects gas below the intrinsic minimum identically to geth (-32000, exact "want" value)', async () => {
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data, gas: '0x10' }, 'latest']),
                rawGeth('eth_call', [{ to: erc20Geth, data, gas: '0x10' }, 'latest']),
            ]);
            expectJsonRpcError(s, -32000, /intrinsic gas too low: have 16, want \d+ \(supplied gas 16\)/);
            expectSameError(s, g);
        });

        it('treats a missing to-address as contract creation identically to geth (-32000 invalid opcode)', async () => {
            // No `to` ⇒ the calldata is run as init code, hitting an invalid opcode.
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ data }, 'latest']),
                rawGeth('eth_call', [{ data }, 'latest']),
            ]);
            expectJsonRpcError(s, -32000, /invalid opcode/);
            expectSameError(s, g);
        });

        it('surfaces a revert with identical code 3, message and ABI-encoded error data on both', async () => {
            const huge = ethers.parseEther('1000000000');
            const data = erc20.transfer(seiAdmin, huge);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data }, 'latest']),
                rawGeth('eth_call', [{ to: erc20Geth, data }, 'latest']),
            ]);
            const err = expectJsonRpcError(s, 3, /execution reverted/i);
            // TestERC20 guards transfers with require(balance >= value, "ERC20: insufficient
            // balance"), which the EVM surfaces as a standard Error(string).
            const expectedData = ethers.concat([
                '0x08c379a0',
                ethers.AbiCoder.defaultAbiCoder().encode(['string'], ['ERC20: insufficient balance']),
            ]);
            expect(err.data).to.equal(expectedData);
            expectSameError(s, g);
        });

        it('[divergence] value sent to a non-payable function: geth → code 3 (data 0x), Sei → -32000', async () => {
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ from: seiAdmin, to: erc20Sei, data, value: '0x1' }, 'latest']),
                rawGeth('eth_call', [{ from: gethAdmin, to: erc20Geth, data, value: '0x1' }, 'latest']),
            ]);
            expectJsonRpcError(g, 3, /execution reverted/i);
            expect(g.error!.data, 'geth attaches empty revert data').to.equal('0x');
            expectJsonRpcError(s, -32000, /execution reverted/i);
            expect(s.error!.data, 'Sei omits the revert data here').to.equal(undefined);
            expect(s.error!.code, 'documented divergence in error code').to.not.equal(g.error!.code);
        });

        it('[divergence] far-future block: both -32000 but different messages', async () => {
            const latest = await sei.getBlockNumber();
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data }, ethers.toQuantity(latest + 1_000_000)]),
                rawGeth('eth_call', [{ to: erc20Geth, data }, '0xffffffff']),
            ]);
            expect(s.error?.code, 'sei code').to.equal(-32000);
            expect(g.error?.code, 'geth code').to.equal(-32000);
            expect(s.error?.code, 'codes still agree').to.equal(g.error?.code);
            expect(s.error?.message).to.match(/not yet available/i);
            expect(g.error?.message).to.match(/header not found/i);
            expect(s.error?.message, 'documented divergence in message').to.not.equal(g.error?.message);
        });

        it('[divergence] unknown block hash: both -32000 but different messages', async () => {
            const zeroHash = '0x' + '00'.repeat(32);
            const data = erc20.balanceOf(seiAdmin);
            const [s, g] = await Promise.all([
                rawSei('eth_call', [{ to: erc20Sei, data }, { blockHash: zeroHash }]),
                rawGeth('eth_call', [{ to: erc20Geth, data }, { blockHash: zeroHash }]),
            ]);
            expect(s.error?.code, 'sei code').to.equal(-32000);
            expect(g.error?.code, 'geth code').to.equal(-32000);
            expect(s.error?.code, 'codes still agree').to.equal(g.error?.code);
            expect(s.error?.message).to.match(/block not found by hash/i);
            expect(g.error?.message).to.match(/header for hash not found/i);
            expect(s.error?.message, 'documented divergence in message').to.not.equal(g.error?.message);
        });

        // A pruning node rejects genesis with -32000; a node whose EVM module postdates
        // genesis rejects it as "evm module does not exist"; a full-history node serves
        // it and returns "0x" since the contract did not exist that early.
        const earlyState = /pruned|evm module does not exist/i;

        it('[Sei-specific] the earliest tag either errors (-32000) or reads genesis state (0x)', async () => {
            const body = await rawSei<string>('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                'earliest',
            ]);
            if (body.error) {
                expect(body.error.code).to.equal(-32000);
                expect(body.error.message).to.match(earlyState);
            } else {
                expect(body.result).to.equal('0x');
            }
        });

        it('[Sei-specific] an early historical block either errors (-32000) or reads pre-deploy state (0x)', async () => {
            const body = await rawSei<string>('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                '0x1',
            ]);
            if (body.error) {
                expect(body.error.code).to.equal(-32000);
                expect(body.error.message).to.match(earlyState);
            } else {
                expect(body.result).to.equal('0x');
            }
        });
    });

    describe('EIP-7702 delegated execution', () => {
        const accountIface = new ethers.Interface(SIMPLE_ACCOUNT_ABI);

        it('dispatches into the delegated implementation and returns 0x for a void executeBatch', async () => {
            const receipt = await setCodeForEOA(alice, [await selfAuthorize(alice, simpleAccountAddress)]);
            expect(receipt?.status).to.equal(1);

            const code = await sei.send('eth_getCode', [alice.address, 'latest']);
            expect(code.toLowerCase()).to.equal(delegationDesignator(simpleAccountAddress));

            const balanceBefore = erc20.decodeBalance(
                await sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(alice.address) }, 'latest']),
            );
            const mintCall = {
                target: erc20Sei,
                value: 0n,
                data: erc20Iface.encodeFunctionData('mint', [alice.address, ethers.parseEther('10')]),
            };
            const batchData = accountIface.encodeFunctionData('executeBatch', [[mintCall]]);

            const result = await sei.send('eth_call', [
                { from: alice.address, to: alice.address, data: batchData },
                'latest',
            ]);
            expect(result).to.equal('0x');

            const balanceAfter = erc20.decodeBalance(
                await sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(alice.address) }, 'latest']),
            );
            expect(balanceAfter).to.equal(balanceBefore);
        });

        it('propagates an inner revert as code 3 execution reverted', async () => {
            const receipt = await setCodeForEOA(aliceRevert, [await selfAuthorize(aliceRevert, simpleAccountAddress)]);
            expect(receipt?.status).to.equal(1);

            const transferCall = {
                target: erc20Sei,
                value: 0n,
                data: erc20.transfer(seiAdmin, ethers.parseEther('1000000000')),
            };
            const batchData = accountIface.encodeFunctionData('executeBatch', [[transferCall]]);

            const err = await captureRpcError(
                sei.send('eth_call', [
                    { from: aliceRevert.address, to: aliceRevert.address, data: batchData },
                    'latest',
                ]),
            );
            expect(err.code).to.equal(3);
            expect(err.message).to.match(/execution reverted/i);
        });
    });
});
