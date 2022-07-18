import { txClient, queryClient, MissingWalletError , registry} from './module'

import { ConnectionEnd } from "./module/types/ibc/core/connection/v1/connection"
import { IdentifiedConnection } from "./module/types/ibc/core/connection/v1/connection"
import { Counterparty } from "./module/types/ibc/core/connection/v1/connection"
import { ClientPaths } from "./module/types/ibc/core/connection/v1/connection"
import { ConnectionPaths } from "./module/types/ibc/core/connection/v1/connection"
import { Version } from "./module/types/ibc/core/connection/v1/connection"
import { Params } from "./module/types/ibc/core/connection/v1/connection"


export { ConnectionEnd, IdentifiedConnection, Counterparty, ClientPaths, ConnectionPaths, Version, Params };

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
				Connection: {},
				Connections: {},
				ClientConnections: {},
				ConnectionClientState: {},
				ConnectionConsensusState: {},
				
				_Structure: {
						ConnectionEnd: getStructure(ConnectionEnd.fromPartial({})),
						IdentifiedConnection: getStructure(IdentifiedConnection.fromPartial({})),
						Counterparty: getStructure(Counterparty.fromPartial({})),
						ClientPaths: getStructure(ClientPaths.fromPartial({})),
						ConnectionPaths: getStructure(ConnectionPaths.fromPartial({})),
						Version: getStructure(Version.fromPartial({})),
						Params: getStructure(Params.fromPartial({})),
						
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
				getConnection: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Connection[JSON.stringify(params)] ?? {}
		},
				getConnections: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Connections[JSON.stringify(params)] ?? {}
		},
				getClientConnections: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ClientConnections[JSON.stringify(params)] ?? {}
		},
				getConnectionClientState: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ConnectionClientState[JSON.stringify(params)] ?? {}
		},
				getConnectionConsensusState: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ConnectionConsensusState[JSON.stringify(params)] ?? {}
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
			console.log('Vuex module: ibc.core.connection.v1 initialized!')
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
		
		
		
		 		
		
		
		async QueryConnection({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryConnection( key.connection_id)).data
				
					
				commit('QUERY', { query: 'Connection', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryConnection', payload: { options: { all }, params: {...key},query }})
				return getters['getConnection']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryConnection API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryConnections({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryConnections(query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryConnections({...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'Connections', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryConnections', payload: { options: { all }, params: {...key},query }})
				return getters['getConnections']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryConnections API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryClientConnections({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryClientConnections( key.client_id)).data
				
					
				commit('QUERY', { query: 'ClientConnections', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryClientConnections', payload: { options: { all }, params: {...key},query }})
				return getters['getClientConnections']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryClientConnections API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryConnectionClientState({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryConnectionClientState( key.connection_id)).data
				
					
				commit('QUERY', { query: 'ConnectionClientState', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryConnectionClientState', payload: { options: { all }, params: {...key},query }})
				return getters['getConnectionClientState']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryConnectionClientState API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryConnectionConsensusState({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryConnectionConsensusState( key.connection_id,  key.revision_number,  key.revision_height)).data
				
					
				commit('QUERY', { query: 'ConnectionConsensusState', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryConnectionConsensusState', payload: { options: { all }, params: {...key},query }})
				return getters['getConnectionConsensusState']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryConnectionConsensusState API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
	}
}
