import { txClient, queryClient, MissingWalletError , registry} from './module'

import { GenesisState_GenMsgs } from "./module/types/cosmwasm/wasm/v1/genesis"
import { Code } from "./module/types/cosmwasm/wasm/v1/genesis"
import { Contract } from "./module/types/cosmwasm/wasm/v1/genesis"
import { Sequence } from "./module/types/cosmwasm/wasm/v1/genesis"
import { StoreCodeProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { InstantiateContractProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { MigrateContractProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { SudoContractProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { ExecuteContractProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { UpdateAdminProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { ClearAdminProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { PinCodesProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { UnpinCodesProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { AccessConfigUpdate } from "./module/types/cosmwasm/wasm/v1/proposal"
import { UpdateInstantiateConfigProposal } from "./module/types/cosmwasm/wasm/v1/proposal"
import { CodeInfoResponse } from "./module/types/cosmwasm/wasm/v1/query"
import { AccessTypeParam } from "./module/types/cosmwasm/wasm/v1/types"
import { AccessConfig } from "./module/types/cosmwasm/wasm/v1/types"
import { Params } from "./module/types/cosmwasm/wasm/v1/types"
import { CodeInfo } from "./module/types/cosmwasm/wasm/v1/types"
import { ContractInfo } from "./module/types/cosmwasm/wasm/v1/types"
import { ContractCodeHistoryEntry } from "./module/types/cosmwasm/wasm/v1/types"
import { AbsoluteTxPosition } from "./module/types/cosmwasm/wasm/v1/types"
import { Model } from "./module/types/cosmwasm/wasm/v1/types"


export { GenesisState_GenMsgs, Code, Contract, Sequence, StoreCodeProposal, InstantiateContractProposal, MigrateContractProposal, SudoContractProposal, ExecuteContractProposal, UpdateAdminProposal, ClearAdminProposal, PinCodesProposal, UnpinCodesProposal, AccessConfigUpdate, UpdateInstantiateConfigProposal, CodeInfoResponse, AccessTypeParam, AccessConfig, Params, CodeInfo, ContractInfo, ContractCodeHistoryEntry, AbsoluteTxPosition, Model };

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
				ContractInfo: {},
				ContractHistory: {},
				ContractsByCode: {},
				AllContractState: {},
				RawContractState: {},
				SmartContractState: {},
				Code: {},
				Codes: {},
				PinnedCodes: {},
				
				_Structure: {
						GenesisState_GenMsgs: getStructure(GenesisState_GenMsgs.fromPartial({})),
						Code: getStructure(Code.fromPartial({})),
						Contract: getStructure(Contract.fromPartial({})),
						Sequence: getStructure(Sequence.fromPartial({})),
						StoreCodeProposal: getStructure(StoreCodeProposal.fromPartial({})),
						InstantiateContractProposal: getStructure(InstantiateContractProposal.fromPartial({})),
						MigrateContractProposal: getStructure(MigrateContractProposal.fromPartial({})),
						SudoContractProposal: getStructure(SudoContractProposal.fromPartial({})),
						ExecuteContractProposal: getStructure(ExecuteContractProposal.fromPartial({})),
						UpdateAdminProposal: getStructure(UpdateAdminProposal.fromPartial({})),
						ClearAdminProposal: getStructure(ClearAdminProposal.fromPartial({})),
						PinCodesProposal: getStructure(PinCodesProposal.fromPartial({})),
						UnpinCodesProposal: getStructure(UnpinCodesProposal.fromPartial({})),
						AccessConfigUpdate: getStructure(AccessConfigUpdate.fromPartial({})),
						UpdateInstantiateConfigProposal: getStructure(UpdateInstantiateConfigProposal.fromPartial({})),
						CodeInfoResponse: getStructure(CodeInfoResponse.fromPartial({})),
						AccessTypeParam: getStructure(AccessTypeParam.fromPartial({})),
						AccessConfig: getStructure(AccessConfig.fromPartial({})),
						Params: getStructure(Params.fromPartial({})),
						CodeInfo: getStructure(CodeInfo.fromPartial({})),
						ContractInfo: getStructure(ContractInfo.fromPartial({})),
						ContractCodeHistoryEntry: getStructure(ContractCodeHistoryEntry.fromPartial({})),
						AbsoluteTxPosition: getStructure(AbsoluteTxPosition.fromPartial({})),
						Model: getStructure(Model.fromPartial({})),
						
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
				getContractInfo: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ContractInfo[JSON.stringify(params)] ?? {}
		},
				getContractHistory: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ContractHistory[JSON.stringify(params)] ?? {}
		},
				getContractsByCode: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ContractsByCode[JSON.stringify(params)] ?? {}
		},
				getAllContractState: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.AllContractState[JSON.stringify(params)] ?? {}
		},
				getRawContractState: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.RawContractState[JSON.stringify(params)] ?? {}
		},
				getSmartContractState: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.SmartContractState[JSON.stringify(params)] ?? {}
		},
				getCode: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Code[JSON.stringify(params)] ?? {}
		},
				getCodes: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Codes[JSON.stringify(params)] ?? {}
		},
				getPinnedCodes: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.PinnedCodes[JSON.stringify(params)] ?? {}
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
			console.log('Vuex module: cosmwasm.wasm.v1 initialized!')
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
		
		
		
		 		
		
		
		async QueryContractInfo({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryContractInfo( key.address)).data
				
					
				commit('QUERY', { query: 'ContractInfo', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryContractInfo', payload: { options: { all }, params: {...key},query }})
				return getters['getContractInfo']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryContractInfo API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryContractHistory({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryContractHistory( key.address, query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryContractHistory( key.address, {...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'ContractHistory', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryContractHistory', payload: { options: { all }, params: {...key},query }})
				return getters['getContractHistory']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryContractHistory API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryContractsByCode({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryContractsByCode( key.code_id, query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryContractsByCode( key.code_id, {...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'ContractsByCode', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryContractsByCode', payload: { options: { all }, params: {...key},query }})
				return getters['getContractsByCode']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryContractsByCode API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryAllContractState({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryAllContractState( key.address, query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryAllContractState( key.address, {...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'AllContractState', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryAllContractState', payload: { options: { all }, params: {...key},query }})
				return getters['getAllContractState']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryAllContractState API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryRawContractState({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryRawContractState( key.address,  key.query_data)).data
				
					
				commit('QUERY', { query: 'RawContractState', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryRawContractState', payload: { options: { all }, params: {...key},query }})
				return getters['getRawContractState']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryRawContractState API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QuerySmartContractState({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.querySmartContractState( key.address,  key.query_data)).data
				
					
				commit('QUERY', { query: 'SmartContractState', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QuerySmartContractState', payload: { options: { all }, params: {...key},query }})
				return getters['getSmartContractState']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QuerySmartContractState API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryCode({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryCode( key.code_id)).data
				
					
				commit('QUERY', { query: 'Code', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryCode', payload: { options: { all }, params: {...key},query }})
				return getters['getCode']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryCode API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryCodes({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryCodes(query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryCodes({...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'Codes', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryCodes', payload: { options: { all }, params: {...key},query }})
				return getters['getCodes']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryCodes API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryPinnedCodes({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryPinnedCodes(query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryPinnedCodes({...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'PinnedCodes', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryPinnedCodes', payload: { options: { all }, params: {...key},query }})
				return getters['getPinnedCodes']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryPinnedCodes API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		async sendMsgInstantiateContract({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgInstantiateContract(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgInstantiateContract:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgInstantiateContract:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgExecuteContract({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgExecuteContract(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgExecuteContract:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgExecuteContract:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgMigrateContract({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgMigrateContract(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgMigrateContract:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgMigrateContract:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgIBCSend({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgIBCSend(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgIBCSend:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgIBCSend:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgStoreCode({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgStoreCode(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgStoreCode:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgStoreCode:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgUpdateAdmin({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgUpdateAdmin(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgUpdateAdmin:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgUpdateAdmin:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgClearAdmin({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgClearAdmin(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgClearAdmin:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgClearAdmin:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgIBCCloseChannel({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgIBCCloseChannel(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgIBCCloseChannel:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgIBCCloseChannel:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		
		async MsgInstantiateContract({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgInstantiateContract(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgInstantiateContract:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgInstantiateContract:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgExecuteContract({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgExecuteContract(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgExecuteContract:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgExecuteContract:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgMigrateContract({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgMigrateContract(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgMigrateContract:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgMigrateContract:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgIBCSend({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgIBCSend(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgIBCSend:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgIBCSend:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgStoreCode({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgStoreCode(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgStoreCode:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgStoreCode:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgUpdateAdmin({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgUpdateAdmin(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgUpdateAdmin:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgUpdateAdmin:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgClearAdmin({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgClearAdmin(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgClearAdmin:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgClearAdmin:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgIBCCloseChannel({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgIBCCloseChannel(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgIBCCloseChannel:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgIBCCloseChannel:Create Could not create message: ' + e.message)
				}
			}
		},
		
	}
}
