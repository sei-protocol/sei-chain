import { txClient, queryClient, MissingWalletError , registry} from './module'
// @ts-ignore
import { SpVuexError } from '@starport/vuex'

import { Params } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { ValidatorHistoricalRewards } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { ValidatorCurrentRewards } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { ValidatorAccumulatedCommission } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { ValidatorOutstandingRewards } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { ValidatorSlashEvent } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { ValidatorSlashEvents } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { FeePool } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { CommunityPoolSpendProposal } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { DelegatorStartingInfo } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { DelegationDelegatorReward } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { CommunityPoolSpendProposalWithDeposit } from "./module/types/cosmos/distribution/v1beta1/distribution"
import { DelegatorWithdrawInfo } from "./module/types/cosmos/distribution/v1beta1/genesis"
import { ValidatorOutstandingRewardsRecord } from "./module/types/cosmos/distribution/v1beta1/genesis"
import { ValidatorAccumulatedCommissionRecord } from "./module/types/cosmos/distribution/v1beta1/genesis"
import { ValidatorHistoricalRewardsRecord } from "./module/types/cosmos/distribution/v1beta1/genesis"
import { ValidatorCurrentRewardsRecord } from "./module/types/cosmos/distribution/v1beta1/genesis"
import { DelegatorStartingInfoRecord } from "./module/types/cosmos/distribution/v1beta1/genesis"
import { ValidatorSlashEventRecord } from "./module/types/cosmos/distribution/v1beta1/genesis"


export { Params, ValidatorHistoricalRewards, ValidatorCurrentRewards, ValidatorAccumulatedCommission, ValidatorOutstandingRewards, ValidatorSlashEvent, ValidatorSlashEvents, FeePool, CommunityPoolSpendProposal, DelegatorStartingInfo, DelegationDelegatorReward, CommunityPoolSpendProposalWithDeposit, DelegatorWithdrawInfo, ValidatorOutstandingRewardsRecord, ValidatorAccumulatedCommissionRecord, ValidatorHistoricalRewardsRecord, ValidatorCurrentRewardsRecord, DelegatorStartingInfoRecord, ValidatorSlashEventRecord };

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
				Params: {},
				ValidatorOutstandingRewards: {},
				ValidatorCommission: {},
				ValidatorSlashes: {},
				DelegationRewards: {},
				DelegationTotalRewards: {},
				DelegatorValidators: {},
				DelegatorWithdrawAddress: {},
				CommunityPool: {},
				
				_Structure: {
						Params: getStructure(Params.fromPartial({})),
						ValidatorHistoricalRewards: getStructure(ValidatorHistoricalRewards.fromPartial({})),
						ValidatorCurrentRewards: getStructure(ValidatorCurrentRewards.fromPartial({})),
						ValidatorAccumulatedCommission: getStructure(ValidatorAccumulatedCommission.fromPartial({})),
						ValidatorOutstandingRewards: getStructure(ValidatorOutstandingRewards.fromPartial({})),
						ValidatorSlashEvent: getStructure(ValidatorSlashEvent.fromPartial({})),
						ValidatorSlashEvents: getStructure(ValidatorSlashEvents.fromPartial({})),
						FeePool: getStructure(FeePool.fromPartial({})),
						CommunityPoolSpendProposal: getStructure(CommunityPoolSpendProposal.fromPartial({})),
						DelegatorStartingInfo: getStructure(DelegatorStartingInfo.fromPartial({})),
						DelegationDelegatorReward: getStructure(DelegationDelegatorReward.fromPartial({})),
						CommunityPoolSpendProposalWithDeposit: getStructure(CommunityPoolSpendProposalWithDeposit.fromPartial({})),
						DelegatorWithdrawInfo: getStructure(DelegatorWithdrawInfo.fromPartial({})),
						ValidatorOutstandingRewardsRecord: getStructure(ValidatorOutstandingRewardsRecord.fromPartial({})),
						ValidatorAccumulatedCommissionRecord: getStructure(ValidatorAccumulatedCommissionRecord.fromPartial({})),
						ValidatorHistoricalRewardsRecord: getStructure(ValidatorHistoricalRewardsRecord.fromPartial({})),
						ValidatorCurrentRewardsRecord: getStructure(ValidatorCurrentRewardsRecord.fromPartial({})),
						DelegatorStartingInfoRecord: getStructure(DelegatorStartingInfoRecord.fromPartial({})),
						ValidatorSlashEventRecord: getStructure(ValidatorSlashEventRecord.fromPartial({})),
						
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
				getParams: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.Params[JSON.stringify(params)] ?? {}
		},
				getValidatorOutstandingRewards: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ValidatorOutstandingRewards[JSON.stringify(params)] ?? {}
		},
				getValidatorCommission: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ValidatorCommission[JSON.stringify(params)] ?? {}
		},
				getValidatorSlashes: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ValidatorSlashes[JSON.stringify(params)] ?? {}
		},
				getDelegationRewards: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.DelegationRewards[JSON.stringify(params)] ?? {}
		},
				getDelegationTotalRewards: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.DelegationTotalRewards[JSON.stringify(params)] ?? {}
		},
				getDelegatorValidators: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.DelegatorValidators[JSON.stringify(params)] ?? {}
		},
				getDelegatorWithdrawAddress: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.DelegatorWithdrawAddress[JSON.stringify(params)] ?? {}
		},
				getCommunityPool: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.CommunityPool[JSON.stringify(params)] ?? {}
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
			console.log('Vuex module: cosmos.distribution.v1beta1 initialized!')
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
					throw new SpVuexError('Subscriptions: ' + e.message)
				}
			})
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
				throw new SpVuexError('QueryClient:QueryParams', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryValidatorOutstandingRewards({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryValidatorOutstandingRewards( key.validator_address)).data
				
					
				commit('QUERY', { query: 'ValidatorOutstandingRewards', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryValidatorOutstandingRewards', payload: { options: { all }, params: {...key},query }})
				return getters['getValidatorOutstandingRewards']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryValidatorOutstandingRewards', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryValidatorCommission({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryValidatorCommission( key.validator_address)).data
				
					
				commit('QUERY', { query: 'ValidatorCommission', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryValidatorCommission', payload: { options: { all }, params: {...key},query }})
				return getters['getValidatorCommission']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryValidatorCommission', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryValidatorSlashes({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryValidatorSlashes( key.validator_address, query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryValidatorSlashes( key.validator_address, {...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'ValidatorSlashes', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryValidatorSlashes', payload: { options: { all }, params: {...key},query }})
				return getters['getValidatorSlashes']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryValidatorSlashes', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryDelegationRewards({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryDelegationRewards( key.delegator_address,  key.validator_address)).data
				
					
				commit('QUERY', { query: 'DelegationRewards', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryDelegationRewards', payload: { options: { all }, params: {...key},query }})
				return getters['getDelegationRewards']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryDelegationRewards', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryDelegationTotalRewards({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryDelegationTotalRewards( key.delegator_address)).data
				
					
				commit('QUERY', { query: 'DelegationTotalRewards', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryDelegationTotalRewards', payload: { options: { all }, params: {...key},query }})
				return getters['getDelegationTotalRewards']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryDelegationTotalRewards', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryDelegatorValidators({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryDelegatorValidators( key.delegator_address)).data
				
					
				commit('QUERY', { query: 'DelegatorValidators', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryDelegatorValidators', payload: { options: { all }, params: {...key},query }})
				return getters['getDelegatorValidators']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryDelegatorValidators', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryDelegatorWithdrawAddress({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryDelegatorWithdrawAddress( key.delegator_address)).data
				
					
				commit('QUERY', { query: 'DelegatorWithdrawAddress', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryDelegatorWithdrawAddress', payload: { options: { all }, params: {...key},query }})
				return getters['getDelegatorWithdrawAddress']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryDelegatorWithdrawAddress', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryCommunityPool({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryCommunityPool()).data
				
					
				commit('QUERY', { query: 'CommunityPool', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryCommunityPool', payload: { options: { all }, params: {...key},query }})
				return getters['getCommunityPool']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new SpVuexError('QueryClient:QueryCommunityPool', 'API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		async sendMsgSetWithdrawAddress({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgSetWithdrawAddress(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgSetWithdrawAddress:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgSetWithdrawAddress:Send', 'Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgWithdrawDelegatorReward({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgWithdrawDelegatorReward(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgWithdrawDelegatorReward:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgWithdrawDelegatorReward:Send', 'Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgWithdrawValidatorCommission({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgWithdrawValidatorCommission(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgWithdrawValidatorCommission:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgWithdrawValidatorCommission:Send', 'Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgFundCommunityPool({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgFundCommunityPool(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgFundCommunityPool:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgFundCommunityPool:Send', 'Could not broadcast Tx: '+ e.message)
				}
			}
		},
		
		async MsgSetWithdrawAddress({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgSetWithdrawAddress(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgSetWithdrawAddress:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgSetWithdrawAddress:Create', 'Could not create message: ' + e.message)
					
				}
			}
		},
		async MsgWithdrawDelegatorReward({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgWithdrawDelegatorReward(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgWithdrawDelegatorReward:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgWithdrawDelegatorReward:Create', 'Could not create message: ' + e.message)
					
				}
			}
		},
		async MsgWithdrawValidatorCommission({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgWithdrawValidatorCommission(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgWithdrawValidatorCommission:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgWithdrawValidatorCommission:Create', 'Could not create message: ' + e.message)
					
				}
			}
		},
		async MsgFundCommunityPool({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgFundCommunityPool(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new SpVuexError('TxClient:MsgFundCommunityPool:Init', 'Could not initialize signing client. Wallet is required.')
				}else{
					throw new SpVuexError('TxClient:MsgFundCommunityPool:Create', 'Could not create message: ' + e.message)
					
				}
			}
		},
		
	}
}
