import { expect } from 'chai';
import { bothProviders, isReachable } from '../utils/providers';
import { rawSei, rawGeth, rawAccountless, expectJsonRpcError } from '../utils/rpc';
import { ADDRESS, ADDRESS_LOWER } from '../utils/format';
import { Endpoints } from '../config/endpoints';

describe('eth_accounts', function () {
    this.timeout(60 * 1000);

    const { sei, geth } = bothProviders();

    describe('Accounts queries', () => {
        it('returns a JSON array', async () => {
            const accounts = await sei.send('eth_accounts', []);
            expect(accounts).to.be.an('array');
        });

        it('every entry is a well-formed 20-byte address', async () => {
            const accounts: string[] = await sei.send('eth_accounts', []);
            for (const acct of accounts) {
                expect(acct, `account ${acct}`).to.match(ADDRESS);
            }
        });

        it('contains no duplicate addresses', async () => {
            const accounts: string[] = await sei.send('eth_accounts', []);
            const lower = accounts.map(a => a.toLowerCase());
            expect(new Set(lower).size).to.equal(lower.length);
        });

        it('returns the same set of accounts across repeated calls', async () => {
            // NOTE: Sei does not guarantee a stable *order* — it serializes the keyring
            // from a Go map, so the order varies call-to-call (geth, by contrast, returns
            // stable insertion order). Consumers must treat the result as a set, not a
            // positional list. We assert the sorted set is stable.
            const results: string[][] = await Promise.all(
                Array.from({ length: 4 }, () => sei.send('eth_accounts', [])),
            );
            const sortedSet = (a: string[]) => [...a].map(x => x.toLowerCase()).sort();
            const baseline = sortedSet(results[0]);
            for (const r of results) {
                expect(sortedSet(r)).to.deep.equal(baseline);
            }
        });
    });

    // ── 2. Schema matching vs the geth reference ────────────────────────────────
    describe('schema matching', () => {
        it('Sei and geth both return arrays of address strings', async () => {
            const [seiAccounts, gethAccounts] = await Promise.all([
                sei.send('eth_accounts', []),
                geth.send('eth_accounts', []),
            ]);

            expect(seiAccounts).to.be.an('array');
            expect(gethAccounts).to.be.an('array');
            for (const acct of [...seiAccounts, ...gethAccounts]) {
                expect(acct).to.be.a('string');
                expect(acct).to.match(ADDRESS);
            }
        });

        it('Sei and geth both serialize addresses in lower-case (non-checksummed) form', async () => {
            const [seiAccounts, gethAccounts] = await Promise.all([
                sei.send('eth_accounts', []),
                geth.send('eth_accounts', []),
            ]);
            for (const acct of [...seiAccounts, ...gethAccounts]) {
                expect(acct, `account ${acct}`).to.match(ADDRESS_LOWER);
            }
        });
    });

    describe('empty / null handling', () => {
        it('a keyless node returns [] (empty array), never null', async function () {
            const body = await rawAccountless<string[]>('eth_accounts', []);
            console.log(body);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result, 'keyless node must encode the empty set as []').to.deep.equal([]);
            expect(body.result).to.not.equal(null);
        });
    });

    describe('wrong params / error handling', () => {
        it('Sei rejects an extra positional parameter with -32602, identically to geth', async () => {
            const [seiBody, gethBody] = await Promise.all([
                rawSei('eth_accounts', ['latest']),
                rawGeth('eth_accounts', ['latest']),
            ]);
            expectJsonRpcError(seiBody, -32602, /too many arguments, want at most 0/i);
            expectJsonRpcError(gethBody, -32602, /too many arguments, want at most 0/i);
            expect(seiBody.error?.code).to.equal(gethBody.error?.code);
            expect(seiBody.error?.message).to.equal(gethBody.error?.message);
        });

        it('Sei rejects non-array params with -32602 non-array args, identically to geth', async () => {
            const [seiBody, gethBody] = await Promise.all([
                rawSei('eth_accounts', 'latest'),
                rawGeth('eth_accounts', 'latest'),
            ]);
            expectJsonRpcError(seiBody, -32602, /non-array args/i);
            expectJsonRpcError(gethBody, -32602, /non-array args/i);
            expect(seiBody.error?.code).to.equal(gethBody.error?.code);
            expect(seiBody.error?.message).to.equal(gethBody.error?.message);
        });
    });
});
