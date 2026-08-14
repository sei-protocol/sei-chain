import { LoadGeneratorConfig, loadGeneratorConfig } from './loadConfig';
import { runSynthetic } from './runSynthetic';

export async function runLoadMain(): Promise<void> {
    const config = loadGeneratorConfig();
    configureProvisioningEnvironment(config);
    if (config.command === 'setup') {
        const { deployFixturesMain } = await import('./deployFixtures');
        await deployFixturesMain();
        return;
    }
    if (config.command === 'provision') {
        const { provisionUsersMain } = await import('./provisionUsers');
        await provisionUsersMain();
        return;
    }
    if (config.type !== 'simulate') {
        await runSynthetic(config);
        return;
    }
    if (!config.execute) {
        console.log(
            `Dry-run simulate: ${config.tps} max tx/s, ` +
                `${config.durationSeconds ?? 'unbounded'} seconds`,
        );
        return;
    }

    process.env.MAX_TPS = String(config.tps);
    if (config.durationSeconds) {
        process.env.RUN_DURATION_SECONDS = String(config.durationSeconds);
    }
    const simulateMode = (process.env.SIMULATE_MODE ?? 'buffered').trim();
    if (simulateMode === 'buffered') {
        const { runBufferedMain } = await import('./runBuffered');
        await runBufferedMain();
        return;
    }
    if (simulateMode === 'corpus') {
        const { runReplayMain } = await import('./runReplay');
        await runReplayMain();
        return;
    }
    throw new Error('SIMULATE_MODE must be buffered or corpus');
}

export function configureProvisioningEnvironment(
    config: Pick<LoadGeneratorConfig, 'command' | 'workerCount'>,
    env: NodeJS.ProcessEnv = process.env,
): void {
    if (config.command !== 'provision') return;
    env.USER_COUNT = env.USER_COUNT?.trim() || String(config.workerCount);
    env.WORKER_COUNT = env.USER_COUNT;
}

if (require.main === module) {
    runLoadMain().catch(error => {
        console.error('Fatal:', error instanceof Error ? error.message : error);
        process.exitCode = 1;
    });
}
