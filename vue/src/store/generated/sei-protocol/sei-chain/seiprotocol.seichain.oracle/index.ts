import { txClient, queryClient, MissingWalletError , registry} from './module'

import { FeederDelegation } from "./module/types/oracle/genesis"
import { PenaltyCounter } from "./module/types/oracle/genesis"
import { Params } from "./module/types/oracle/oracle"
import { Denom } from "./module/types/oracle/oracle"
import { AggregateExchangeRatePrevote } from "./module/types/oracle/oracle"
import { AggregateExchangeRateVote } from "./module/types/oracle/oracle"
import { ExchangeRateTuple } from "./module/types/oracle/oracle"
import { OracleExchangeRate } from "./module/types/oracle/oracle"
import { PriceSnapshotItem } from "./module/types/oracle/oracle"
import { PriceSnapshot } from "./module/types/oracle/oracle"
import { OracleTwap } from "./module/types/oracle/oracle"
import { VotePenaltyCounter } from "./module/types/oracle/oracle"
import { DenomOracleExchangeRatePair } from "./module/types/oracle/query"


export { FeederDelegation, PenaltyCounter, Params, Denom, AggregateExchangeRatePrevote, AggregateExchangeRateVote, ExchangeRateTuple, OracleExchangeRate, PriceSnapshotItem, PriceSnapshot, OracleTwap, VotePenaltyCounter, DenomOracleExchangeRatePair };

async function initTxClient(vuexGetters) {
	return await txClient(vuexGetters['common/wallet/signer'], {
		addr: vuexGetters['common/env/apiTendermint']
	})
}

async function initQueryClient(vuexGetters) {
	return await queryClient({
		addr: vuexGetters['common/env/apiCosmos']
	})
}

function mergeResults(value, next_values) {
	for (let prop of Object.keys(next_values)) {
		if (Array.isArray(next_values[prop])) {
			value[prop]=[...value[prop], ...next_values[prop]]
		}else{
			value[prop]=next_values[prop]
		}
	}
	return value
}

function getStructure(template) {
	let structure = { fields: [] }
	for (const [key, value] of Object.entries(template)) {
		let field: any = {}
		field.name = key
		field.type = typeof value
		structure.fields.push(field)
	}
	return structure
}

const getDefaultState = () => {
	return {
				ExchangeRate: {},
				ExchangeRates: {},
				Actives: {},
				VoteTargets: {},
				PriceSnapshotHistory: {},
				Twaps: {},
				FeederDelegation: {},
				VotePenaltyCounter: {},
				AggregatePrevote: {},
				AggregatePrevotes: {},
				AggregateVote: {},
				AggregateVotes: {},
				Params: {},
				
				_Structure: {
						FeederDelegation: getStructure(FeederDelegation.fromPartial({})),
						PenaltyCounter: getStructure(PenaltyCounter.fromPartial({})),
						Params: getStructure(Params.fromPartial({})),
						Denom: getStructure(Denom.fromPartial({})),
						AggregateExchangeRatePrevote: getStructure(AggregateExchangeRatePrevote.fromPartial({})),
						AggregateExchangeRateVote: getStructure(AggregateExchangeRateVote.fromPartial({})),
						ExchangeRateTuple: getStructure(ExchangeRateTuple.fromPartial({})),
						OracleExchangeRate: getStructure(OracleExchangeRate.fromPartial({})),
						PriceSnapshotItem: getStructure(PriceSnapshotItem.fromPartial({})),
						PriceSnapshot: getStructure(PriceSnapshot.fromPartial({})),
						OracleTwap: getStructure(OracleTwap.fromPartial({})),
						VotePenaltyCounter: getStructure(VotePenaltyCounter.fromPartial({})),
						DenomOracleExchangeRatePair: getStructure(DenomOracleExchangeRatePair.fromPartial({})),
						
		},
		_Registry: registry,
		_Subscriptions: new Set(),
	}
}

// initial state
const state = getDefaultState()

export default {
	namespaced: true,
	state,
	mutations: {
		RESET_STATE(state) {
			Object.assign(state, getDefaultState())
		},
		QUERY(state, { query, key, value }) {
			state[query][JSON.stringify(key)] = value
		},
		SUBSCRIBE(state, subscription) {
			state._Subscriptions.add(JSON.stringify(subscription))
		},
		UNSUBSCRIBE(state, subscription) {
			state._Subscriptions.delete(JSON.stringify(subscription))
		}
	},
	getters: {
				getExchangeRate: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ExchangeRate[JSON.stringify(params)] ?? {}
		},
				getExchangeRates: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ExchangeRates[JSON.stringify(params)] ?? {}
		},
				getActives: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Actives[JSON.stringify(params)] ?? {}
		},
				getVoteTargets: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.VoteTargets[JSON.stringify(params)] ?? {}
		},
				getPriceSnapshotHistory: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.PriceSnapshotHistory[JSON.stringify(params)] ?? {}
		},
				getTwaps: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Twaps[JSON.stringify(params)] ?? {}
		},
				getFeederDelegation: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.FeederDelegation[JSON.stringify(params)] ?? {}
		},
				getVotePenaltyCounter: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.VotePenaltyCounter[JSON.stringify(params)] ?? {}
		},
				getAggregatePrevote: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.AggregatePrevote[JSON.stringify(params)] ?? {}
		},
				getAggregatePrevotes: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.AggregatePrevotes[JSON.stringify(params)] ?? {}
		},
				getAggregateVote: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.AggregateVote[JSON.stringify(params)] ?? {}
		},
				getAggregateVotes: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.AggregateVotes[JSON.stringify(params)] ?? {}
		},
				getParams: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Params[JSON.stringify(params)] ?? {}
		},
				
		getTypeStructure: (state) => (type) => {
			return state._Structure[type].fields
		},
		getRegistry: (state) => {
			return state._Registry
		}
	},
	actions: {
		init({ dispatch, rootGetters }) {
			console.log('Vuex module: seiprotocol.seichain.oracle initialized!')
			if (rootGetters['common/env/client']) {
				rootGetters['common/env/client'].on('newblock', () => {
					dispatch('StoreUpdate')
				})
			}
		},
		resetState({ commit }) {
			commit('RESET_STATE')
		},
		unsubscribe({ commit }, subscription) {
			commit('UNSUBSCRIBE', subscription)
		},
		async StoreUpdate({ state, dispatch }) {
			state._Subscriptions.forEach(async (subscription) => {
				try {
					const sub=JSON.parse(subscription)
					await dispatch(sub.action, sub.payload)
				}catch(e) {
					throw new Error('Subscriptions: ' + e.message)
				}
			})
		},
		
		
		
		 		
		
		
		async QueryExchangeRate({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryExchangeRate( key.denom)).data
				
					
				commit('QUERY', { query: 'ExchangeRate', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryExchangeRate', payload: { options: { all }, params: {...key},query }})
				return getters['getExchangeRate']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryExchangeRate API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryExchangeRates({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryExchangeRates()).data
				
					
				commit('QUERY', { query: 'ExchangeRates', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryExchangeRates', payload: { options: { all }, params: {...key},query }})
				return getters['getExchangeRates']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryExchangeRates API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryActives({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryActives()).data
				
					
				commit('QUERY', { query: 'Actives', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryActives', payload: { options: { all }, params: {...key},query }})
				return getters['getActives']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryActives API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryVoteTargets({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryVoteTargets()).data
				
					
				commit('QUERY', { query: 'VoteTargets', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryVoteTargets', payload: { options: { all }, params: {...key},query }})
				return getters['getVoteTargets']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryVoteTargets API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryPriceSnapshotHistory({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryPriceSnapshotHistory()).data
				
					
				commit('QUERY', { query: 'PriceSnapshotHistory', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryPriceSnapshotHistory', payload: { options: { all }, params: {...key},query }})
				return getters['getPriceSnapshotHistory']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryPriceSnapshotHistory API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryTwaps({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryTwaps(query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryTwaps({...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'Twaps', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryTwaps', payload: { options: { all }, params: {...key},query }})
				return getters['getTwaps']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryTwaps API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryFeederDelegation({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryFeederDelegation( key.validator_addr)).data
				
					
				commit('QUERY', { query: 'FeederDelegation', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryFeederDelegation', payload: { options: { all }, params: {...key},query }})
				return getters['getFeederDelegation']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryFeederDelegation API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryVotePenaltyCounter({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryVotePenaltyCounter( key.validator_addr)).data
				
					
				commit('QUERY', { query: 'VotePenaltyCounter', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryVotePenaltyCounter', payload: { options: { all }, params: {...key},query }})
				return getters['getVotePenaltyCounter']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryVotePenaltyCounter API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryAggregatePrevote({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryAggregatePrevote( key.validator_addr)).data
				
					
				commit('QUERY', { query: 'AggregatePrevote', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryAggregatePrevote', payload: { options: { all }, params: {...key},query }})
				return getters['getAggregatePrevote']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryAggregatePrevote API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryAggregatePrevotes({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryAggregatePrevotes()).data
				
					
				commit('QUERY', { query: 'AggregatePrevotes', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryAggregatePrevotes', payload: { options: { all }, params: {...key},query }})
				return getters['getAggregatePrevotes']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryAggregatePrevotes API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryAggregateVote({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryAggregateVote( key.validator_addr)).data
				
					
				commit('QUERY', { query: 'AggregateVote', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryAggregateVote', payload: { options: { all }, params: {...key},query }})
				return getters['getAggregateVote']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryAggregateVote API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryAggregateVotes({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryAggregateVotes()).data
				
					
				commit('QUERY', { query: 'AggregateVotes', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryAggregateVotes', payload: { options: { all }, params: {...key},query }})
				return getters['getAggregateVotes']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryAggregateVotes API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryParams({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryParams()).data
				
					
				commit('QUERY', { query: 'Params', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryParams', payload: { options: { all }, params: {...key},query }})
				return getters['getParams']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryParams API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		async sendMsgAggregateExchangeRatePrevote({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgAggregateExchangeRatePrevote(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgAggregateExchangeRatePrevote:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgAggregateExchangeRatePrevote:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgAggregateExchangeRateVote({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgAggregateExchangeRateVote(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgAggregateExchangeRateVote:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgAggregateExchangeRateVote:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgAggregateExchangeRateCombinedVote({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgAggregateExchangeRateCombinedVote(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgAggregateExchangeRateCombinedVote:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgAggregateExchangeRateCombinedVote:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgDelegateFeedConsent({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgDelegateFeedConsent(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgDelegateFeedConsent:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgDelegateFeedConsent:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		
		async MsgAggregateExchangeRatePrevote({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgAggregateExchangeRatePrevote(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgAggregateExchangeRatePrevote:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgAggregateExchangeRatePrevote:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgAggregateExchangeRateVote({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgAggregateExchangeRateVote(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgAggregateExchangeRateVote:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgAggregateExchangeRateVote:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgAggregateExchangeRateCombinedVote({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgAggregateExchangeRateCombinedVote(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgAggregateExchangeRateCombinedVote:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgAggregateExchangeRateCombinedVote:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgDelegateFeedConsent({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgDelegateFeedConsent(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgDelegateFeedConsent:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgDelegateFeedConsent:Create Could not create message: ' + e.message)
				}
			}
		},
		
	}
}
