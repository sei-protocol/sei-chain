#!/usr/bin/env python3
# Summarizes a running CLOB bench from Prometheus: chain TPS, per-validator capacity and CPU, main-loop
# and OCC step costs per transaction, and Go runtime blocking.
# Usage: PROM=http://127.0.0.1:19090 NAMESPACE=... CHAIN_ID=... measure.py <label> [window, default 5m]
import json,os,urllib.parse,urllib.request,sys
PROM=os.environ.get('PROM','http://127.0.0.1:19090')
NS=os.environ['NAMESPACE']
CHAIN=os.environ['CHAIN_ID']
F='chain_id="%s",sei_role="validator"'%CHAIN
W=sys.argv[2] if len(sys.argv)>2 else '5m'
def q(e):
  e=e.replace('NAMESPACE_',NS).replace('CHAIN_',CHAIN)
  return json.load(urllib.request.urlopen(PROM+'/api/v1/query?'+urllib.parse.urlencode({'query':e})))['data']['result']
def d(e): return {x['metric'].get('pod','?')[-3]:float(x['value'][1]) for x in q(e)}
def line(name,v,fmt='%.1f'): print('  %-20s'%name,' '.join(('%s='+fmt)%(k,v[k]) for k in sorted(v)))
txs='sum by (pod) (rate(sei_chain_giga_evmonly_txs_executed_total{%s}[%s]))'%(F,W)
t=d(txs); med=sorted(t.values())[len(t)//2]
print('== %s  chain TPS %.1fk'%(sys.argv[1],med/1000))
blk='sum by (pod) (rate(tendermint_internal_autobahn_data_latency_count{namespace="NAMESPACE_",resource="blocks",stage="execute"}[%s]))'%W
busy=d('sum by (pod) (rate(sei_chain_autobahn_main_loop_phase_duration_seconds_total{%s,phase=~"execution|storage"}[%s]))'%(F,W))
cap={k:t[k]/busy[k]/1000 for k in t if k in busy and busy[k]>0}
line('capacity k',cap,'%.0f')
line('cpu',d('sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="NAMESPACE_",container="seid",pod=~"CHAIN_.*"}[%s]))'%W))
line('ingest k/s',d('sum by (pod) (rate(tendermint_internal_autobahn_producer_inserts{namespace="NAMESPACE_",result!~"pending_full|full"}[%s]))/1000'%W))
sb=q('sum(rate(sei_chain_giga_evmonly_txs_executed_total{%s}[%s]))/sum(rate(tendermint_internal_autobahn_data_latency_count{namespace="NAMESPACE_",resource="blocks",stage="execute"}[%s]))'%(F,W,W))
print('  txs per lane block %.0f'%float(sb[0]['value'][1]) if sb else '')
tot='sum(rate(sei_chain_giga_evmonly_txs_executed_total{%s}[%s]))'%(F,W)
for name,m in [('main loop','autobahn_main_loop_phase_duration_seconds_total'),('executor','evmonly_block_phase_duration_seconds_total'),('bg commit','giga_state_commit_phase_duration_seconds_total')]:
  r=q('1e9*sum by (phase) (rate({__name__=~"(sei_chain_)?%s",%s}[%s])) / on() group_left() %s'%(m,F,W,tot))
  print('  %-10s ns/tx '%name+', '.join('%s=%.0f'%(x['metric']['phase'],float(x['value'][1])) for x in sorted(r,key=lambda x:-float(x['value'][1])) if float(x['value'][1])>=50))
ws=d('sum by (pod) (rate(sei_chain_giga_occ_speculate_worker_seconds_total{%s}[%s]))'%(F,W))
sp=d('sum by (pod) (rate(sei_chain_evmonly_block_phase_duration_seconds_total{%s,phase="occ_speculate"}[%s]))'%(F,W))
line('active workers',{k:ws[k]/sp[k] for k in sp if k in ws and sp[k]>0})
sel='chain_id="%s"'%CHAIN
line('mutex wait cores',d('sum by (pod) (rate(go_sync_mutex_wait_total_seconds_total{%s}[%s]))'%(sel,W)))
line('runnable cores',d('sum by (pod) (rate(go_sched_latencies_seconds_sum{%s}[%s]))'%(sel,W)))
line('gc cores',d('sum by (pod) (rate(go_cpu_classes_gc_total_cpu_seconds_total{%s}[%s]))'%(sel,W)))
line('idle cores',d('sum by (pod) (rate(go_cpu_classes_idle_cpu_seconds_total{%s}[%s]))'%(sel,W)))
line('storage miss/tx',d('sum by (pod) (rate(sei_chain_view_manager_misses_total{%s,cache="storage"}[%s])) / on(pod) %s'%(F,W,txs)),'%.2f')
line('backpressure ns/tx',d('1e9*sum by (pod) (rate(sei_chain_view_manager_view_phase_duration_seconds_total{%s,cache="storage",phase="lifecycle_backpressure"}[%s])) / on(pod) %s'%(F,W,txs)),'%.0f')
line('flush busy frac',d('sum by (pod) (rate(sei_chain_view_manager_lifecycle_phase_duration_seconds_total{%s,cache="storage",phase=~"flush.*"}[%s]))'%(F,W)),'%.2f')
r=q('1e9*sum by (step) (rate(sei_chain_giga_occ_step_seconds_total{%s}[%s])) / on() group_left() %s'%(F,W,tot))
lw={x['metric']['step']:float(x['value'][1]) for x in q('sum by (step) (rate(sei_chain_giga_occ_step_longest_worker_seconds_total{%s}[%s]))'%(F,W))}
wall={x['metric']['step']:float(x['value'][1]) for x in q('sum by (step) (rate(sei_chain_giga_occ_step_seconds_total{%s}[%s]))'%(F,W))}
if r: print('  occ steps ns/tx (longest-worker/wall) '+', '.join('%s=%.0f(%.2f)'%(x['metric']['step'],float(x['value'][1]),lw.get(x['metric']['step'],0)/max(wall.get(x['metric']['step'],1e-12),1e-12)) for x in sorted(r,key=lambda x:-float(x['value'][1]))))
r=q('sum by (phase) (rate(sei_chain_view_manager_lifecycle_phase_duration_seconds_total{%s,cache="storage"}[%s]))/count(count by (pod) (sei_chain_view_manager_lifecycle_phase_duration_seconds_total{%s,cache="storage"}))'%(F,W,F))
if r: print('  storage lifecycle busy frac/node '+', '.join('%s=%.2f'%(x['metric']['phase'],float(x['value'][1])) for x in sorted(r,key=lambda x:-float(x['value'][1]))))
