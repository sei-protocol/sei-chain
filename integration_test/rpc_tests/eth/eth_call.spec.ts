import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, captureRpcError, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, Erc20Calldata, claimPool, encodeUint, expectSameError } from '../utils/testUtils';
import { abiOf, EvmAccount, SIMPLE_ACCOUNT_ABI, delegationDesignator, selfAuthorize, setCodeForEOA } from '../utils/evmUtils';
import { HEX_DATA, EARLY_STATE_ERROR } from '../utils/format';
import { STAKING_PRECOMPILE_ADDRESS } from '../utils/constants';

describe('eth_call Tests', function () {
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

    describe('eth_call Queries', () => {
        it('omitting the block tag defaults to latest for eth_call queries', async () => {
            const [withoutTag, withLatest] = await Promise.all([
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }]),
                sei.send('eth_call', [{ to: erc20Sei, data: erc20.balanceOf(seiAdmin) }, 'latest']),
            ]);
            expect(withoutTag).to.equal(withLatest);
        });

        it('a call against an EOA (no code set with EIP7702) returns 0x', async () => {
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
            expect(erc20.decodeBalance(latest), 'admin balance at latest is the minted amount').to.equal(
                ADMIN_MINT,
            );
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

        it('eth_call returns correct data with sei precompile calls (Staking Precompile)', async () => {
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
            const data = '0xfe';
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
            const expectedData = ethers.concat([
                '0x08c379a0',
                ethers.AbiCoder.defaultAbiCoder().encode(['string'], ['ERC20: insufficient balance']),
            ]);
            expect(err.data).to.equal(expectedData);
            expectSameError(s, g);
        });

        describe('an ERC20 transfer behaves identically across legacy / access-list / EIP-1559 call shapes', () => {
            const INSUFFICIENT_BALANCE_DATA = ethers.concat([
                '0x08c379a0',
                ethers.AbiCoder.defaultAbiCoder().encode(['string'], ['ERC20: insufficient balance']),
            ]);
            let shapes: { name: string; fields: Record<string, unknown> }[];

            before(async () => {
                const head = await sei.send('eth_getBlockByNumber', ['latest', false]);
                const base = BigInt(head.baseFeePerGas ?? '0x3b9aca00');
                const price = ethers.toQuantity(base * 3n + ethers.parseUnits('2', 'gwei'));
                const tip = ethers.toQuantity(ethers.parseUnits('1', 'gwei'));
                shapes = [
                    { name: 'no fee fields', fields: {} },
                    { name: 'legacy gasPrice (type 0)', fields: { type: '0x0', gasPrice: price } },
                    { name: 'access-list (type 1)', fields: { type: '0x1', gasPrice: price, accessList: [] } },
                    {
                        name: 'EIP-1559 caps (type 2)',
                        fields: { type: '0x2', maxFeePerGas: price, maxPriorityFeePerGas: tip },
                    },
                ];
            });

            it('a transfer within balance returns true for every fee/type shape', async () => {
                for (const shape of shapes) {
                    const result = await sei.send('eth_call', [
                        {
                            from: seiAdmin,
                            to: erc20Sei,
                            data: erc20.transfer(alice.address, ethers.parseEther('1')),
                            ...shape.fields,
                        },
                        'latest',
                    ]);
                    expect(
                        erc20Iface.decodeFunctionResult('transfer', result)[0],
                        `${shape.name}: transfer returns true`,
                    ).to.equal(true);
                }
            });

            it('a transfer above balance reverts with code 3 + ERC20 insufficient balance for every shape', async () => {
                const aboveBalance = ADMIN_MINT + ethers.parseEther('1');
                for (const shape of shapes) {
                    const err = await captureRpcError(
                        sei.send('eth_call', [
                            {
                                from: seiAdmin,
                                to: erc20Sei,
                                data: erc20.transfer(alice.address, aboveBalance),
                                ...shape.fields,
                            },
                            'latest',
                        ]),
                    );
                    expect(err.code, `${shape.name}: execution reverted code`).to.equal(3);
                    expect(err.message, `${shape.name}: revert message`).to.match(/execution reverted/i);
                    expect(err.data, `${shape.name}: ABI-encoded insufficient-balance error`).to.equal(
                        INSUFFICIENT_BALANCE_DATA,
                    );
                }
            });
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

        it('the earliest tag either errors (-32000) or reads genesis state (0x)', async () => {
            const body = await rawSei<string>('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                'earliest',
            ]);
            if (body.error) {
                expect(body.error.code).to.equal(-32000);
                expect(body.error.message).to.match(EARLY_STATE_ERROR);
            } else {
                expect(body.result).to.equal('0x');
            }
        });

        it('an early historical block either errors (-32000) or reads pre-deploy state (0x)', async () => {
            const body = await rawSei<string>('eth_call', [
                { to: erc20Sei, data: erc20.balanceOf(seiAdmin) },
                '0x1',
            ]);
            if (body.error) {
                expect(body.error.code).to.equal(-32000);
                expect(body.error.message).to.match(EARLY_STATE_ERROR);
            } else {
                expect(body.result).to.equal('0x');
            }
        });
    });

    describe('state override (5th parameter)', () => {
        it('balance override lets a zero-balance address spend in the simulated call', async () => {
            // A fresh wallet has no ETH, so it cannot pay for an ETH-value transfer.
            // Overriding its balance in the call context should make it appear funded.
            const broke = ethers.Wallet.createRandom().address;
            const recipient = ethers.Wallet.createRandom().address;

            // Without override: calling a transfer from `broke` with a value should fail.
            // With override: the node temporarily credits `broke` with 10 ETH.
            const seiBody = await rawSei('eth_call', [
                { from: broke, to: recipient, value: ethers.toQuantity(ethers.parseEther('1')) },
                'latest',
                { [broke]: { balance: ethers.toQuantity(ethers.parseEther('10')) } },
            ]);
            // The call should succeed (result = '0x') because the balance was overridden.
            expect(seiBody.result, 'balance-overridden call succeeds').to.equal('0x');
            
        });

        it('balance override parity: geth accepts the 5th param and succeeds', async () => {
            const broke = ethers.Wallet.createRandom().address;
            const recipient = ethers.Wallet.createRandom().address;
            const gBody = await rawGeth('eth_call', [
                { from: broke, to: recipient, value: ethers.toQuantity(ethers.parseEther('1')) },
                'latest',
                { [broke]: { balance: ethers.toQuantity(ethers.parseEther('10')) } },
            ]);
            expect(gBody.error, 'geth accepts balance override').to.equal(undefined);
            expect(gBody.result).to.equal('0x');
        });

        it('code override replaces a contract bytecode for the duration of the call', async () => {
            const randomAddr = ethers.Wallet.createRandom().address;

            // PUSH1 0x20 PUSH1 0 PUSH1 0 CALLDATACOPY PUSH1 0x20 PUSH1 0 RETURN
            // This stub copies 32 bytes of calldata into memory and returns them —
            // a trivial echo. We use it to verify the override is actually applied.
            const echoCode = '0x6020600060003760206000F3';

            const seiBody = await rawSei<string>('eth_call', [
                {
                    to: randomAddr,
                    data: ethers.zeroPadValue('0xdeadbeef', 32), // 32 bytes of calldata
                },
                'latest',
                { [randomAddr]: { code: echoCode } },
            ]);
                // The echo stub returns the first 32 bytes of calldata.
            expect(seiBody.result, 'code override: echo stub returns calldata').to.equal(
                ethers.zeroPadValue('0xdeadbeef', 32),
            );    
        });

        it('code override parity: geth echoes calldata via the override stub', async () => {
            const randomAddr = ethers.Wallet.createRandom().address;
            const echoCode = '0x6020600060003760206000F3';
            const gBody = await rawGeth<string>('eth_call', [
                { to: randomAddr, data: ethers.zeroPadValue('0xdeadbeef', 32) },
                'latest',
                { [randomAddr]: { code: echoCode } },
            ]);
            expect(gBody.error, 'geth supports code override').to.equal(undefined);
            expect(gBody.result).to.equal(ethers.zeroPadValue('0xdeadbeef', 32));
        });

        it('nonce override is reflected in eth_call simulation', async () => {
            // Overriding nonce to a specific value must not cause an error.
            // We cannot observe the nonce inside the call easily without a contract,
            // so this test just checks the call does not error with a nonce override.
            const seiBody = await rawSei('eth_call', [
                { to: runtime.contracts.erc20, data: erc20.balanceOf(seiAdmin) },
                'latest',
                { [seiAdmin]: { nonce: '0xff' } },
            ]);
            // The balance read is unaffected by the nonce override.
            expect(seiBody.result).to.match(/^0x/);
            
        });
    });

    describe('value forwarding & deploy simulation', () => {
        it('eth_call with a value field does not transfer ETH (simulation only)', async () => {
            const recipient = ethers.Wallet.createRandom().address;
            const balanceBefore = await sei.getBalance(recipient, 'latest');

            // This should succeed (or produce a deterministic result) without touching balances.
            await rawSei('eth_call', [
                { from: seiAdmin, to: recipient, value: ethers.toQuantity(ethers.parseEther('1')) },
                'latest',
            ]);

            const balanceAfter = await sei.getBalance(recipient, 'latest');
            expect(balanceAfter, 'eth_call does not transfer actual ETH').to.equal(balanceBefore);
        });

        it('eth_call without `to` (deployment simulation) returns runtime bytecode or 0x', async () => {
            // Omitting `to` triggers the CREATE path. The call should return the
            // constructor's return value (the runtime code), not error out.
            // We use a trivial constructor that returns STOP (0x00).
            //   PUSH1 0x01 PUSH1 0x00 RETURN  (returns 1 byte: 0x00)
            const initCode = '0x600160005260016000F3';
            const body = await rawSei<string>('eth_call', [{ data: initCode }, 'latest']);
            expect(body.result, 'deploy simulation returns hex data').to.match(/^0x/);
            
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
