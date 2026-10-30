#!/usr/bin/env python3
# Summarizes a running CLOB bench from Prometheus: chain TPS, per-validator capacity and CPU, main-loop
# costs per transaction, and FlatKV cache and flush load.
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
t=d(txs)
if not t:
  sys.exit('no executed transactions for chain %s in namespace %s over %s'%(CHAIN,NS,W))
med=sorted(t.values())[len(t)//2]
print('== %s  chain TPS %.1fk'%(sys.argv[1],med/1000))
blk='sum by (pod) (rate(tendermint_internal_autobahn_data_latency_count{namespace="NAMESPACE_",resource="blocks",stage="execute"}[%s]))'%W
busy=d('sum by (pod) (rate(sei_chain_autobahn_main_loop_phase_duration_seconds_total{%s,phase=~"execution|storage"}[%s]))'%(F,W))
cap={k:t[k]/busy[k]/1000 for k in t if k in busy and busy[k]>0}
line('capacity k',cap,'%.0f')
if cap: print('  capacity k summary   min=%.0f median=%.0f'%(min(cap.values()),sorted(cap.values())[len(cap)//2]))
line('cpu',d('sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="NAMESPACE_",container="seid",pod=~"CHAIN_.*"}[%s]))'%W))
line('ingest k/s',d('sum by (pod) (rate(tendermint_internal_autobahn_producer_inserts{namespace="NAMESPACE_",result!~"pending_full|full"}[%s]))/1000'%W))
sb=q('sum(rate(sei_chain_giga_evmonly_txs_executed_total{%s}[%s]))/sum(rate(tendermint_internal_autobahn_data_latency_count{namespace="NAMESPACE_",resource="blocks",stage="execute"}[%s]))'%(F,W,W))
print('  txs per lane block %.0f'%float(sb[0]['value'][1]) if sb else '')
tot='sum(rate(sei_chain_giga_evmonly_txs_executed_total{%s}[%s]))'%(F,W)
for name,m in [('main loop','autobahn_main_loop_phase_duration_seconds_total'),('executor','evmonly_block_phase_duration_seconds_total'),('bg commit','giga_state_commit_phase_duration_seconds_total')]:
  r=q('1e9*sum by (phase) (rate({__name__=~"(sei_chain_)?%s",%s}[%s])) / on() group_left() %s'%(m,F,W,tot))
  print('  %-10s ns/tx '%name+', '.join('%s=%.0f'%(x['metric']['phase'],float(x['value'][1])) for x in sorted(r,key=lambda x:-float(x['value'][1])) if float(x['value'][1])>=50))
sel='chain_id="%s"'%CHAIN
line('storage miss/tx',d('sum by (pod) (rate(sei_chain_view_manager_misses_total{%s,cache="storage"}[%s])) / on(pod) %s'%(F,W,txs)),'%.2f')
line('backpressure ns/tx',d('1e9*sum by (pod) (rate(sei_chain_view_manager_view_phase_duration_seconds_total{%s,cache="storage",phase="lifecycle_backpressure"}[%s])) / on(pod) %s'%(F,W,txs)),'%.0f')
line('flush busy frac',d('sum by (pod) (rate(sei_chain_view_manager_lifecycle_phase_duration_seconds_total{%s,cache="storage",phase=~"flush.*"}[%s]))'%(F,W)),'%.2f')
r=q('sum by (phase) (rate(sei_chain_view_manager_lifecycle_phase_duration_seconds_total{%s,cache="storage"}[%s]))/count(count by (pod) (sei_chain_view_manager_lifecycle_phase_duration_seconds_total{%s,cache="storage"}))'%(F,W,F))
if r: print('  storage lifecycle busy frac/node '+', '.join('%s=%.2f'%(x['metric']['phase'],float(x['value'][1])) for x in sorted(r,key=lambda x:-float(x['value'][1]))))
eo={x['metric'].get('outcome','?'):float(x['value'][1]) for x in q('sum by (outcome) (rate(sei_chain_giga_occ_early_txs_total{%s}[%s]))'%(F,W))}
tsum=sum(t.values())
if eo: print('  early txs per tx      '+', '.join('%s=%.2f'%(k,v/tsum) for k,v in sorted(eo.items())))
