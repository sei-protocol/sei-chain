/**
 * Cosmos REST reads.
 *
 * The two facts the verify phase cannot work without — whether the upgrade ran,
 * and which modules the chain thinks exist — come from here rather than from the
 * upgrade precompile. A released binary does not necessarily register that
 * precompile (arctic-1 on v6.6.1 answers `0x` at 0x…1015), and a gate that
 * silently cannot tell whether the upgrade happened is worse than no gate.
 */
import { resolveTarget } from '../config/targets';

export interface RestResult<T> {
    ok: boolean;
    status: number;
    body: T | undefined;
    /** The error message a non-2xx response carried, when it had one. */
    message?: string;
}

async function restGet<T>(pathAndQuery: string): Promise<RestResult<T>> {
    const url = `${resolveTarget().restUrl}${pathAndQuery}`;
    const res = await fetch(url, { headers: { accept: 'application/json' } });
    const text = await res.text();
    let parsed: unknown;
    try {
        parsed = JSON.parse(text);
    } catch {
        parsed = undefined;
    }
    if (!res.ok) {
        const message =
            (parsed as { message?: string } | undefined)?.message ?? text.slice(0, 200);
        return { ok: false, status: res.status, body: undefined, message };
    }
    return { ok: true, status: res.status, body: parsed as T };
}

export interface ModuleVersion {
    name: string;
    version: string;
}

/**
 * The height the named upgrade was applied at, or 0 if it has not been applied.
 * Proven against a real past upgrade: arctic-1 reports v6.3.0 at 138093082.
 */
export async function appliedPlanHeight(planName: string): Promise<number> {
    const result = await restGet<{ height: string }>(
        `/cosmos/upgrade/v1beta1/applied_plan/${encodeURIComponent(planName)}`,
    );
    if (!result.ok || !result.body) {
        throw new Error(
            `applied_plan/${planName} failed (HTTP ${result.status}): ${result.message ?? 'no body'}. ` +
                'Set UPGRADE_TEST_REST to a Cosmos REST endpoint for this network.',
        );
    }
    return Number(result.body.height ?? '0');
}

/** The currently scheduled upgrade plan, or undefined when none is scheduled. */
export async function currentPlan(): Promise<{ name: string; height: string } | undefined> {
    const result = await restGet<{ plan: { name: string; height: string } | null }>(
        '/cosmos/upgrade/v1beta1/current_plan',
    );
    if (!result.ok || !result.body) return undefined;
    return result.body.plan ?? undefined;
}

/** Every module version the chain has stored, sorted by name. */
export async function moduleVersions(): Promise<ModuleVersion[]> {
    const result = await restGet<{ module_versions: ModuleVersion[] }>(
        '/cosmos/upgrade/v1beta1/module_versions',
    );
    if (!result.ok || !result.body) {
        throw new Error(
            `module_versions failed (HTTP ${result.status}): ${result.message ?? 'no body'}`,
        );
    }
    return [...(result.body.module_versions ?? [])].sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * A single module's version as a token: `v<version>` when present, `absent`
 * once the upgrade has deleted it. The upgrade keeper answers a named lookup
 * for a missing module with a not-found error rather than an empty list, so an
 * error here is the expected post-removal answer, not a transport failure.
 */
export async function moduleVersionToken(moduleName: string): Promise<string> {
    const result = await restGet<{ module_versions: ModuleVersion[] }>(
        `/cosmos/upgrade/v1beta1/module_versions?module_name=${encodeURIComponent(moduleName)}`,
    );
    if (result.ok && result.body) {
        const found = (result.body.module_versions ?? []).find(m => m.name === moduleName);
        return found ? `v${found.version}` : 'absent';
    }
    if (/not found/i.test(result.message ?? '')) return 'absent';
    return `error(HTTP ${result.status}: ${(result.message ?? '').slice(0, 120)})`;
}

/**
 * Whether a Cosmos query still serves data or answers with the deprecation
 * error. Returns `ok` or `deprecated` or a truncated description of anything
 * else, so the value is stable enough to compare across runs.
 */
export async function queryToken(pathAndQuery: string): Promise<string> {
    const result = await restGet<unknown>(pathAndQuery);
    if (result.ok) return 'ok';
    const message = result.message ?? '';
    if (/is deprecated/i.test(message)) return 'deprecated';
    return `error(HTTP ${result.status}: ${message.slice(0, 120)})`;
}

/**
 * The chain's latest Cosmos block height. Recorded alongside the EVM height
 * because applied_plan reports a Cosmos height, and the two must be compared on
 * the same clock.
 */
export async function latestCosmosHeight(): Promise<number> {
    const result = await restGet<{ block: { header: { height: string } } }>(
        '/cosmos/base/tendermint/v1beta1/blocks/latest',
    );
    if (!result.ok || !result.body) {
        throw new Error(
            `blocks/latest failed (HTTP ${result.status}): ${result.message ?? 'no body'}`,
        );
    }
    return Number(result.body.block.header.height);
}

/** The seid version the endpoint reports, for the artifact's provenance. */
export async function applicationVersion(): Promise<string> {
    const result = await restGet<{ application_version?: { version?: string } }>(
        '/cosmos/base/tendermint/v1beta1/node_info',
    );
    return result.body?.application_version?.version ?? 'unknown';
}
