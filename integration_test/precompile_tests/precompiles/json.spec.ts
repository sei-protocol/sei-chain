/**
 * json precompile (0x…1003) — end-to-end semantics against a live Sei chain.
 *
 * Pure functions of calldata (no chain state), so the parity oracle is
 * client-side: expected outputs are computed in TS from the same JSON input.
 * The interesting behaviors to pin are the quote-stripping asymmetry
 * (extractAsBytes / extractAsBytesFromArray strip outer quotes from string
 * values; extractAsBytesList keeps them) and the uint256 parsing rules.
 *
 * The 2^16 array-boundary cases need ~131KB inputs (~13.1M gas at 100 gas per
 * arg byte) that exceed the 10M eth_call simulation cap — they stay covered by
 * the Go unit tests (json_test.go) and are intentionally not repeated here.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
    expectVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

const bytes = (s: string): Uint8Array => ethers.toUtf8Bytes(s);
const text = (b: string): string => ethers.toUtf8String(b);

describe('json precompile (0x1003)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const jsonIface = precompileInterface('json');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let json: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        json = precompileContract('json', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path (client-side parity)', () => {
        it('extractAsBytes strips outer quotes from string values', async () => {
            const ret: string = await json.extractAsBytes(bytes('{"key":"value"}'), 'key');
            expect(text(ret)).to.equal('value');
        });

        it('extractAsBytes returns non-string values as raw JSON', async () => {
            const ret: string = await json.extractAsBytes(
                bytes('{"obj":{"a":1,"b":[2,3]}}'),
                'obj',
            );
            expect(text(ret)).to.equal('{"a":1,"b":[2,3]}');
        });

        it('extractAsBytesList keeps quotes on string elements (asymmetry by design)', async () => {
            const ret: string[] = await json.extractAsBytesList(
                bytes('{"list":["1","2"]}'),
                'list',
            );
            expect(ret.map(text)).to.deep.equal(['"1"', '"2"']);
        });

        it('extractAsBytesList returns an empty array for []', async () => {
            const ret: string[] = await json.extractAsBytesList(bytes('{"list":[]}'), 'list');
            expect(ret).to.deep.equal([]);
        });

        it('extractAsUint256 parses quoted and unquoted numbers identically', async () => {
            const [quoted, unquoted] = await Promise.all([
                json.extractAsUint256(bytes('{"n":"12345"}'), 'n'),
                json.extractAsUint256(bytes('{"n":12345}'), 'n'),
            ]);
            expect(quoted).to.equal(12345n);
            expect(unquoted).to.equal(12345n);
        });

        it('extractAsUint256 handles the uint256 maximum', async () => {
            const max = 2n ** 256n - 1n;
            const ret: bigint = await json.extractAsUint256(bytes(`{"n":"${max}"}`), 'n');
            expect(ret).to.equal(max);
        });

        it('extractAsBytesFromArray indexes a top-level array, stripping string quotes', async () => {
            const input = bytes('["x",7]');
            const [el0, el1] = await Promise.all([
                json.extractAsBytesFromArray(input, 0),
                json.extractAsBytesFromArray(input, 1),
            ]);
            expect(text(el0)).to.equal('x');
            expect(text(el1)).to.equal('7');
        });
    });

    describe('error handling', () => {
        it('missing key reverts', async () => {
            await expectExecutionReverted(
                json.extractAsBytes(bytes('{"a":1}'), 'missing'),
                'json.extractAsBytes with a missing key',
            );
        });

        it('invalid JSON reverts', async () => {
            await expectExecutionReverted(
                json.extractAsBytes(bytes('not json at all'), 'key'),
                'json.extractAsBytes with invalid JSON',
            );
        });

        it('out-of-bounds array index reverts', async () => {
            await expectExecutionReverted(
                json.extractAsBytesFromArray(bytes('[1]'), 5),
                'json.extractAsBytesFromArray out of bounds',
            );
        });

        it('extractAsUint256 rejects a number longer than 100 characters', async () => {
            await expectExecutionReverted(
                json.extractAsUint256(bytes(`{"n":"${'9'.repeat(101)}"}`), 'n'),
                'json.extractAsUint256 with a >100 char number',
            );
        });

        it('extractAsUint256 rejects a non-numeric value', async () => {
            await expectExecutionReverted(
                json.extractAsUint256(bytes('{"n":"abc"}'), 'n'),
                'json.extractAsUint256 with a non-numeric value',
            );
        });

        it('a mined failing tx carries the exact Go error in its VmError', async () => {
            const data = jsonIface.encodeFunctionData('extractAsBytes', [
                bytes('{"a":1}'),
                'missing',
            ]);
            await expectVmError(
                admin.wallet.sendTransaction({
                    to: PRECOMPILE_ADDRESSES.json,
                    data,
                    gasLimit: 200_000,
                }),
                'input does not contain key',
            );
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        const input = bytes('{"key":"value"}');

        it('responds through a real CALL from contract bytecode', async () => {
            const data = jsonIface.encodeFunctionData('extractAsBytes', [input, 'key']);
            const ret: string = await caller.callTarget.staticCall(PRECOMPILE_ADDRESSES.json, data);
            const [decoded] = jsonIface.decodeFunctionResult('extractAsBytes', ret);
            expect(text(decoded)).to.equal('value');
        });

        it('responds under STATICCALL', async () => {
            const data = jsonIface.encodeFunctionData('extractAsBytes', [input, 'key']);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.json,
                data,
            );
            const [decoded] = jsonIface.decodeFunctionResult('extractAsBytes', ret);
            expect(text(decoded)).to.equal('value');
        });

        it('responds under DELEGATECALL (json has no delegatecall guard)', async () => {
            const data = jsonIface.encodeFunctionData('extractAsBytes', [input, 'key']);
            const ret: string = await caller.delegatecallTarget.staticCall(
                PRECOMPILE_ADDRESSES.json,
                data,
            );
            const [decoded] = jsonIface.decodeFunctionResult('extractAsBytes', ret);
            expect(text(decoded)).to.equal('value');
        });

        it('rejects value on every method (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.json,
                    data: jsonIface.encodeFunctionData('extractAsBytes', [input, 'key']),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'value-bearing call must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });
    });
});
