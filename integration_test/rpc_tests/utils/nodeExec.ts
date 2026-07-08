import util from 'node:util';
import { DOCKER_NODE, SEID_ENV } from './constants';
import { isInProcess } from './cosmosUtils';

const exec = util.promisify(require('node:child_process').exec);

/**
 * Run a `seid` subcommand against the local Sei node, returning its stdout/stderr.
 *
 * Two backends, selected by env so the docker path is untouched:
 *   - In-process arm (`SEI_IN_PROCESS=1`): the runner puts a `seid` shim on PATH that
 *     targets the harness node (via SEID_HOME/SEID_NODE), so a bare `seid <args>` reaches
 *     it — there is no container to `docker exec` into.
 *   - Default (docker): exec inside the sei-node-0 container.
 *
 * `args` is the command tail after `seid` (e.g. `q staking validators -o json`). Callers
 * that shell out to read-only cosmos-side queries should use this rather than a hard-coded
 * `docker exec`, so they resolve in both arms. A tx needing keyring/--evm-rpc flags (which
 * differ per arm) branches on isInProcess directly instead.
 */
export async function seidNodeExec(args: string): Promise<{ stdout: string; stderr: string }> {
    if (isInProcess()) {
        return exec(`seid ${args}`);
    }
    return exec(`docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && seid ${args}'`);
}
