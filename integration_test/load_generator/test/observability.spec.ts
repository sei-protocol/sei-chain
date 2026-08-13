import fs from 'node:fs';
import path from 'node:path';
import { expect } from 'chai';

interface DashboardTarget {
    expr?: string;
}

interface DashboardPanel {
    title?: string;
    targets?: DashboardTarget[];
}

interface Dashboard {
    panels: DashboardPanel[];
    uid: string;
}

describe('replay observability', () => {
    const dashboardPath = path.resolve('observability/grafana/dashboards/pacific-replay.json');

    it('ships a valid dashboard covering every custom replay metric', () => {
        const dashboard = JSON.parse(fs.readFileSync(dashboardPath, 'utf8')) as Dashboard;
        expect(dashboard.uid).to.equal('pacific-replay');
        expect(dashboard.panels).to.have.length.greaterThan(10);

        const expressions = dashboard.panels
            .flatMap(panel => panel.targets ?? [])
            .map(target => target.expr ?? '')
            .join('\n');
        const expectedMetrics = [
            'pacific_replay_offered_transactions_total',
            'pacific_replay_adapted_transactions_total',
            'pacific_replay_transaction_outcomes_total',
            'pacific_replay_skipped_transactions_total',
            'pacific_replay_bytes_total',
            'pacific_replay_gas_used_total',
            'pacific_replay_trace_transactions_total',
            'pacific_replay_source_trace_operations_total',
            'pacific_replay_submission_seconds_bucket',
            'pacific_replay_pending_transactions',
            'pacific_replay_buffer_seconds',
            'pacific_replay_collected_source_height',
            'pacific_replay_replayed_source_height',
            'pacific_replay_paused',
            'pacific_replay_run_remaining_seconds',
            'pacific_replay_run_info',
        ];
        for (const metric of expectedMetrics) {
            expect(expressions, `${metric} is not displayed`).to.include(metric);
        }
    });

    it('keeps panel identifiers and titles unique', () => {
        const dashboard = JSON.parse(fs.readFileSync(dashboardPath, 'utf8')) as Dashboard;
        const ids = dashboard.panels.map(panel => (panel as DashboardPanel & { id: number }).id);
        const titles = dashboard.panels.map(panel => panel.title);
        expect(new Set(ids).size).to.equal(ids.length);
        expect(new Set(titles).size).to.equal(titles.length);
    });

    it('ships a dashboard for generated workloads', () => {
        const dashboard = JSON.parse(
            fs.readFileSync(
                path.resolve('observability/grafana/dashboards/sei-load-generator.json'),
                'utf8',
            ),
        ) as Dashboard;
        expect(dashboard.uid).to.equal('sei-load-generator');
        const expressions = dashboard.panels
            .flatMap(panel => panel.targets ?? [])
            .map(target => target.expr ?? '')
            .join('\n');
        for (const metric of [
            'sei_loadgen_target_tps',
            'sei_loadgen_transactions_total',
            'sei_loadgen_transaction_seconds_bucket',
            'sei_loadgen_pending_transactions',
        ]) {
            expect(expressions, `${metric} is not displayed`).to.include(metric);
        }
    });

    it('binds dashboard services to localhost', () => {
        const compose = fs.readFileSync(path.resolve('observability/docker-compose.yml'), 'utf8');
        expect(compose).to.include('127.0.0.1:9090:9090');
        expect(compose).to.include('127.0.0.1:3000:3000');
        expect(compose).to.include('GRAFANA_ADMIN_PASSWORD:?set GRAFANA_ADMIN_PASSWORD');
    });
});
