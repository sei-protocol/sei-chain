import { txClient, queryClient, MissingWalletError , registry} from './module'

import { Tx } from "./module/types/cosmos/tx/v1beta1/tx"
import { TxRaw } from "./module/types/cosmos/tx/v1beta1/tx"
import { SignDoc } from "./module/types/cosmos/tx/v1beta1/tx"
import { TxBody } from "./module/types/cosmos/tx/v1beta1/tx"
import { AuthInfo } from "./module/types/cosmos/tx/v1beta1/tx"
import { SignerInfo } from "./module/types/cosmos/tx/v1beta1/tx"
import { ModeInfo } from "./module/types/cosmos/tx/v1beta1/tx"
import { ModeInfo_Single } from "./module/types/cosmos/tx/v1beta1/tx"
import { ModeInfo_Multi } from "./module/types/cosmos/tx/v1beta1/tx"
import { Fee } from "./module/types/cosmos/tx/v1beta1/tx"


export { Tx, TxRaw, SignDoc, TxBody, AuthInfo, SignerInfo, ModeInfo, ModeInfo_Single, ModeInfo_Multi, Fee };

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
				Simulate: {},
				GetTx: {},
				BroadcastTx: {},
				GetTxsEvent: {},
				GetBlockWithTxs: {},
				
				_Structure: {
						Tx: getStructure(Tx.fromPartial({})),
						TxRaw: getStructure(TxRaw.fromPartial({})),
						SignDoc: getStructure(SignDoc.fromPartial({})),
						TxBody: getStructure(TxBody.fromPartial({})),
						AuthInfo: getStructure(AuthInfo.fromPartial({})),
						SignerInfo: getStructure(SignerInfo.fromPartial({})),
						ModeInfo: getStructure(ModeInfo.fromPartial({})),
						ModeInfo_Single: getStructure(ModeInfo_Single.fromPartial({})),
						ModeInfo_Multi: getStructure(ModeInfo_Multi.fromPartial({})),
						Fee: getStructure(Fee.fromPartial({})),
						
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
				getSimulate: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Simulate[JSON.stringify(params)] ?? {}
		},
				getGetTx: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetTx[JSON.stringify(params)] ?? {}
		},
				getBroadcastTx: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.BroadcastTx[JSON.stringify(params)] ?? {}
		},
				getGetTxsEvent: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetTxsEvent[JSON.stringify(params)] ?? {}
		},
				getGetBlockWithTxs: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetBlockWithTxs[JSON.stringify(params)] ?? {}
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
			console.log('Vuex module: cosmos.tx.v1beta1 initialized!')
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
		
		
		
		 		
		
		
		async ServiceSimulate({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.serviceSimulate({...key})).data
				
					
				commit('QUERY', { query: 'Simulate', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'ServiceSimulate', payload: { options: { all }, params: {...key},query }})
				return getters['getSimulate']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:ServiceSimulate API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async ServiceGetTx({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.serviceGetTx( key.hash)).data
				
					
				commit('QUERY', { query: 'GetTx', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'ServiceGetTx', payload: { options: { all }, params: {...key},query }})
				return getters['getGetTx']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:ServiceGetTx API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async ServiceBroadcastTx({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.serviceBroadcastTx({...key})).data
				
					
				commit('QUERY', { query: 'BroadcastTx', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'ServiceBroadcastTx', payload: { options: { all }, params: {...key},query }})
				return getters['getBroadcastTx']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:ServiceBroadcastTx API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async ServiceGetTxsEvent({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.serviceGetTxsEvent(query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.serviceGetTxsEvent({...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'GetTxsEvent', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'ServiceGetTxsEvent', payload: { options: { all }, params: {...key},query }})
				return getters['getGetTxsEvent']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:ServiceGetTxsEvent API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async ServiceGetBlockWithTxs({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.serviceGetBlockWithTxs( key.height, query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.serviceGetBlockWithTxs( key.height, {...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'GetBlockWithTxs', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'ServiceGetBlockWithTxs', payload: { options: { all }, params: {...key},query }})
				return getters['getGetBlockWithTxs']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:ServiceGetBlockWithTxs API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
	}
}
