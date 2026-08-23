/**
 * authz precompile (0x…100E) — grant queries against a live Sei chain.
 *
 * All three methods are views. The precompile cannot create grants, so the
 * non-empty fixture is a staking.grantStakingAuthorization (three
 * StakeAuthorization grants: delegate / redelegate / undelegate). Empty-pair
 * checks use a separate associated pair that never grants. Parity oracle is
 * LCD GET /cosmos/authz/v1beta1/grants?granter=&grantee=.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, waitUntil } from '../utils/chainUtils';
import { EvmAccount, associateViaTx } from '../utils/evmUtils';
import { bondedValidators, cosmosRest } from '../utils/cosmosUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
} from '../utils/precompileUtils';
import { readRuntimeState, claimPool, RuntimeState } from '../utils/testUtils';

const emptyPage = new Uint8Array();
const MAX_TOKENS = 1_000_000n;

interface LcdGrant {
    authorization?: { '@type'?: string };
    expiration?: string;
}

interface LcdGrants {
    grants?: LcdGrant[] | null;
}

const authorizationJson = (authorization: string): string => ethers.toUtf8String(authorization);

const lcdExpirationUnix = (expiration: string): bigint => {
    const trimmed = expiration.replace(/\.\d+(Z|[+-]\d{2}:\d{2})$/, '$1');
    return BigInt(Math.floor(Date.parse(trimmed) / 1000));
};

describe('authz precompile (0x100E)', function () {
    this.timeout(180 * 1000);

    const provider = seiRpc();
    const authzIface = precompileInterface('authz');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let authz: ethers.Contract;
    let staking: ethers.Contract;
    let caller: ethers.Contract;
    let granter: EvmAccount;
    let grantee: EvmAccount;
    let validator: string;
    let expiration: bigint;

    before(async () => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        authz = precompileContract('authz', admin.wallet);
        staking = precompileContract('staking', admin.wallet);
        caller = callerContract(runtime, admin.wallet);

        [granter, grantee] = claimPool(runtime, provider, 2, 'authz:grant');
        await associateViaTx(granter);
        await associateViaTx(grantee);

        const validators = await bondedValidators();
        expect(validators.length, 'devnet must have a bonded validator').to.be.greaterThan(0);
        validator = validators[0];

        expiration = BigInt(Math.floor(Date.now() / 1000) + 86_400);
        const tx = await (staking.connect(granter.wallet) as ethers.Contract).grantStakingAuthorization(
            grantee.address,
            [validator],
            MAX_TOKENS,
            expiration,
            { gasLimit: 1_000_000 },
        );
        expect((await tx.wait())!.status, 'grantStakingAuthorization tx must succeed').to.equal(1);

        await waitUntil(
            async () => {
                const resp = await authz.grants(granter.address, grantee.address, '', emptyPage);
                return resp.grants.length > 0 ? true : null;
            },
            { timeoutMs: 30_000, label: 'authz grants after staking grant' },
        );
    });

    describe('empty grants for a pair with no grant', () => {
        let unusedGranter: EvmAccount;
        let unusedGrantee: EvmAccount;

        before(async () => {
            [unusedGranter, unusedGrantee] = claimPool(runtime, provider, 2, 'authz:empty');
            await associateViaTx(unusedGranter);
            await associateViaTx(unusedGrantee);
        });

        it('grants returns an empty list', async () => {
            const resp = await authz.grants(
                unusedGranter.address,
                unusedGrantee.address,
                '',
                emptyPage,
            );
            expect(resp.grants.length).to.equal(0);
        });

        it('granterGrants and granteeGrants do not include the pair', async () => {
            const [fromGranter, fromGrantee] = await Promise.all([
                authz.granterGrants(unusedGranter.address, emptyPage),
                authz.granteeGrants(unusedGrantee.address, emptyPage),
            ]);
            const pair = (g: { granter: string; grantee: string }) =>
                g.granter === unusedGranter.seiAddress() && g.grantee === unusedGrantee.seiAddress();
            expect([...fromGranter.grants].some(pair)).to.equal(false);
            expect([...fromGrantee.grants].some(pair)).to.equal(false);
        });
    });

    describe('non-empty after staking grant', () => {
        it('grants(granter, grantee, "", emptyPage) is non-empty', async () => {
            const resp = await authz.grants(granter.address, grantee.address, '', emptyPage);
            expect(resp.grants.length, 'staking grant creates StakeAuthorization rows').to.be.greaterThan(
                0,
            );
            for (const grant of resp.grants) {
                const json = authorizationJson(grant.authorization);
                expect(json).to.include('@type');
                expect(json).to.include('StakeAuthorization');
                expect(grant.expiration).to.equal(expiration);
            }
        });

        it('granterGrants includes the granter/grantee pair', async () => {
            const resp = await authz.granterGrants(granter.address, emptyPage);
            const pair = [...resp.grants].filter(
                (g: { granter: string; grantee: string }) =>
                    g.granter === granter.seiAddress() && g.grantee === grantee.seiAddress(),
            );
            expect(pair.length, 'granterGrants must include the staking grant pair').to.be.greaterThan(
                0,
            );
            for (const grant of pair) {
                expect(authorizationJson(grant.authorization)).to.include('StakeAuthorization');
                expect(grant.expiration).to.equal(expiration);
            }
        });

        it('granteeGrants includes the granter/grantee pair', async () => {
            const resp = await authz.granteeGrants(grantee.address, emptyPage);
            const pair = [...resp.grants].filter(
                (g: { granter: string; grantee: string }) =>
                    g.granter === granter.seiAddress() && g.grantee === grantee.seiAddress(),
            );
            expect(pair.length, 'granteeGrants must include the staking grant pair').to.be.greaterThan(
                0,
            );
            for (const grant of pair) {
                expect(authorizationJson(grant.authorization)).to.include('StakeAuthorization');
                expect(grant.expiration).to.equal(expiration);
            }
        });
    });

    describe('LCD parity / unassociated granter-grantee / STATICCALL', () => {
        it('LCD /cosmos/authz/v1beta1/grants matches grants() count and expiration', async () => {
            const path =
                `/cosmos/authz/v1beta1/grants?granter=${encodeURIComponent(granter.seiAddress())}` +
                `&grantee=${encodeURIComponent(grantee.seiAddress())}`;
            const [viaPrecompile, lcd] = await Promise.all([
                authz.grants(granter.address, grantee.address, '', emptyPage),
                cosmosRest<LcdGrants>(path),
            ]);
            const lcdGrants = lcd.grants ?? [];
            expect(lcdGrants.length, 'LCD grant count').to.equal(viaPrecompile.grants.length);
            expect(lcdGrants.length).to.be.greaterThan(0);

            const precompileTypes = [...viaPrecompile.grants]
                .map((g: { authorization: string }) => JSON.parse(authorizationJson(g.authorization))['@type'])
                .sort();
            const lcdTypes = lcdGrants.map(g => g.authorization?.['@type']).sort();
            expect(precompileTypes).to.deep.equal(lcdTypes);

            // The two lists are not guaranteed to be in the same order, and all
            // three grants in this fixture share one expiration, so an
            // index-by-index comparison would pass even if the orders diverged.
            // Compare them as sorted sets instead.
            const precompileExpirations = [...viaPrecompile.grants]
                .map((g: { expiration: bigint }) => g.expiration)
                .sort();
            const lcdExpirations = lcdGrants
                .map(g => lcdExpirationUnix(g.expiration ?? ''))
                .sort();
            expect(precompileExpirations).to.deep.equal(lcdExpirations);
        });

        it('grants reverts for an unassociated granter or grantee', async () => {
            const unassociated = EvmAccount.random(provider);
            await expectExecutionReverted(
                authz.grants(unassociated.address, grantee.address, '', emptyPage),
                'authz.grants with an unassociated granter',
            );
            await expectExecutionReverted(
                authz.grants(granter.address, unassociated.address, '', emptyPage),
                'authz.grants with an unassociated grantee',
            );
        });

        it('granterGrants and granteeGrants revert for an unassociated address', async () => {
            const unassociated = EvmAccount.random(provider);
            await expectExecutionReverted(
                authz.granterGrants(unassociated.address, emptyPage),
                'authz.granterGrants with an unassociated granter',
            );
            await expectExecutionReverted(
                authz.granteeGrants(unassociated.address, emptyPage),
                'authz.granteeGrants with an unassociated grantee',
            );
        });

        it('grants responds under STATICCALL', async () => {
            const data = authzIface.encodeFunctionData('grants', [
                granter.address,
                grantee.address,
                '',
                emptyPage,
            ]);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.authz,
                data,
            );
            const [decoded] = authzIface.decodeFunctionResult('grants', ret);
            const direct = await authz.grants(granter.address, grantee.address, '', emptyPage);
            expect(decoded.grants.length).to.equal(direct.grants.length);
            expect(decoded.grants.length).to.be.greaterThan(0);
        });
    });
});
