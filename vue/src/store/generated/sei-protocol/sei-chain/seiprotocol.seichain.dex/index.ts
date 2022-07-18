import { txClient, queryClient, MissingWalletError , registry} from './module'

import { AssetIBCInfo } from "./module/types/dex/asset_list"
import { AssetMetadata } from "./module/types/dex/asset_list"
import { ContractInfo } from "./module/types/dex/contract"
import { RegisterPairsProposal } from "./module/types/dex/gov"
import { UpdateTickSizeProposal } from "./module/types/dex/gov"
import { AddAssetMetadataProposal } from "./module/types/dex/gov"
import { LongBook } from "./module/types/dex/long_book"
import { Order } from "./module/types/dex/order"
import { Cancellation } from "./module/types/dex/order"
import { ActiveOrders } from "./module/types/dex/order"
import { OrderEntry } from "./module/types/dex/order_entry"
import { Allocation } from "./module/types/dex/order_entry"
import { Pair } from "./module/types/dex/pair"
import { BatchContractPair } from "./module/types/dex/pair"
import { Params } from "./module/types/dex/params"
import { Price } from "./module/types/dex/price"
import { PriceCandlestick } from "./module/types/dex/price"
import { SettlementEntry } from "./module/types/dex/settlement"
import { Settlements } from "./module/types/dex/settlement"
import { ShortBook } from "./module/types/dex/short_book"
import { TickSize } from "./module/types/dex/tick_size"
import { Twap } from "./module/types/dex/twap"


export { AssetIBCInfo, AssetMetadata, ContractInfo, RegisterPairsProposal, UpdateTickSizeProposal, AddAssetMetadataProposal, LongBook, Order, Cancellation, ActiveOrders, OrderEntry, Allocation, Pair, BatchContractPair, Params, Price, PriceCandlestick, SettlementEntry, Settlements, ShortBook, TickSize, Twap };

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
				LongBook: {},
				LongBookAll: {},
				ShortBook: {},
				ShortBookAll: {},
				GetSettlements: {},
				GetPrices: {},
				GetTwaps: {},
				AssetMetadata: {},
				AssetList: {},
				GetRegisteredPairs: {},
				GetOrders: {},
				GetOrderByID: {},
				GetHistoricalPrices: {},
				
				_Structure: {
						AssetIBCInfo: getStructure(AssetIBCInfo.fromPartial({})),
						AssetMetadata: getStructure(AssetMetadata.fromPartial({})),
						ContractInfo: getStructure(ContractInfo.fromPartial({})),
						RegisterPairsProposal: getStructure(RegisterPairsProposal.fromPartial({})),
						UpdateTickSizeProposal: getStructure(UpdateTickSizeProposal.fromPartial({})),
						AddAssetMetadataProposal: getStructure(AddAssetMetadataProposal.fromPartial({})),
						LongBook: getStructure(LongBook.fromPartial({})),
						Order: getStructure(Order.fromPartial({})),
						Cancellation: getStructure(Cancellation.fromPartial({})),
						ActiveOrders: getStructure(ActiveOrders.fromPartial({})),
						OrderEntry: getStructure(OrderEntry.fromPartial({})),
						Allocation: getStructure(Allocation.fromPartial({})),
						Pair: getStructure(Pair.fromPartial({})),
						BatchContractPair: getStructure(BatchContractPair.fromPartial({})),
						Params: getStructure(Params.fromPartial({})),
						Price: getStructure(Price.fromPartial({})),
						PriceCandlestick: getStructure(PriceCandlestick.fromPartial({})),
						SettlementEntry: getStructure(SettlementEntry.fromPartial({})),
						Settlements: getStructure(Settlements.fromPartial({})),
						ShortBook: getStructure(ShortBook.fromPartial({})),
						TickSize: getStructure(TickSize.fromPartial({})),
						Twap: getStructure(Twap.fromPartial({})),
						
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
				getLongBook: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.LongBook[JSON.stringify(params)] ?? {}
		},
				getLongBookAll: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.LongBookAll[JSON.stringify(params)] ?? {}
		},
				getShortBook: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ShortBook[JSON.stringify(params)] ?? {}
		},
				getShortBookAll: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.ShortBookAll[JSON.stringify(params)] ?? {}
		},
				getGetSettlements: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetSettlements[JSON.stringify(params)] ?? {}
		},
				getGetPrices: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetPrices[JSON.stringify(params)] ?? {}
		},
				getGetTwaps: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetTwaps[JSON.stringify(params)] ?? {}
		},
				getAssetMetadata: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.AssetMetadata[JSON.stringify(params)] ?? {}
		},
				getAssetList: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.AssetList[JSON.stringify(params)] ?? {}
		},
				getGetRegisteredPairs: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetRegisteredPairs[JSON.stringify(params)] ?? {}
		},
				getGetOrders: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetOrders[JSON.stringify(params)] ?? {}
		},
				getGetOrderByID: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetOrderByID[JSON.stringify(params)] ?? {}
		},
				getGetHistoricalPrices: (state) => (params = { params: {}}) => {
					if (!(<any> params).query) {
						(<any> params).query=null
					}
			return state.GetHistoricalPrices[JSON.stringify(params)] ?? {}
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
			console.log('Vuex module: seiprotocol.seichain.dex initialized!')
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
		
		
		
		
		 		
		
		
		async QueryLongBook({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryLongBook( key.contractAddr,  key.priceDenom,  key.assetDenom,  key.price)).data
				
					
				commit('QUERY', { query: 'LongBook', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryLongBook', payload: { options: { all }, params: {...key},query }})
				return getters['getLongBook']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryLongBook API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryLongBookAll({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryLongBookAll( key.contractAddr,  key.priceDenom,  key.assetDenom, query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryLongBookAll( key.contractAddr,  key.priceDenom,  key.assetDenom, {...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'LongBookAll', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryLongBookAll', payload: { options: { all }, params: {...key},query }})
				return getters['getLongBookAll']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryLongBookAll API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryShortBook({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryShortBook( key.contractAddr,  key.priceDenom,  key.assetDenom,  key.price)).data
				
					
				commit('QUERY', { query: 'ShortBook', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryShortBook', payload: { options: { all }, params: {...key},query }})
				return getters['getShortBook']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryShortBook API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryShortBookAll({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryShortBookAll( key.contractAddr,  key.priceDenom,  key.assetDenom, query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryShortBookAll( key.contractAddr,  key.priceDenom,  key.assetDenom, {...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'ShortBookAll', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryShortBookAll', payload: { options: { all }, params: {...key},query }})
				return getters['getShortBookAll']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryShortBookAll API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryGetSettlements({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryGetSettlements( key.contractAddr,  key.priceDenom,  key.assetDenom,  key.orderId)).data
				
					
				commit('QUERY', { query: 'GetSettlements', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryGetSettlements', payload: { options: { all }, params: {...key},query }})
				return getters['getGetSettlements']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryGetSettlements API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryGetPrices({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryGetPrices( key.contractAddr,  key.priceDenom,  key.assetDenom)).data
				
					
				commit('QUERY', { query: 'GetPrices', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryGetPrices', payload: { options: { all }, params: {...key},query }})
				return getters['getGetPrices']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryGetPrices API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryGetTwaps({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryGetTwaps( key.contractAddr,  key.lookbackSeconds)).data
				
					
				commit('QUERY', { query: 'GetTwaps', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryGetTwaps', payload: { options: { all }, params: {...key},query }})
				return getters['getGetTwaps']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryGetTwaps API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryAssetMetadata({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryAssetMetadata( key.denom)).data
				
					
				commit('QUERY', { query: 'AssetMetadata', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryAssetMetadata', payload: { options: { all }, params: {...key},query }})
				return getters['getAssetMetadata']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryAssetMetadata API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryAssetList({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryAssetList()).data
				
					
				commit('QUERY', { query: 'AssetList', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryAssetList', payload: { options: { all }, params: {...key},query }})
				return getters['getAssetList']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryAssetList API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryGetRegisteredPairs({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryGetRegisteredPairs(query)).data
				
					
				while (all && (<any> value).pagination && (<any> value).pagination.next_key!=null) {
					let next_values=(await queryClient.queryGetRegisteredPairs({...query, 'pagination.key':(<any> value).pagination.next_key})).data
					value = mergeResults(value, next_values);
				}
				commit('QUERY', { query: 'GetRegisteredPairs', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryGetRegisteredPairs', payload: { options: { all }, params: {...key},query }})
				return getters['getGetRegisteredPairs']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryGetRegisteredPairs API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryGetOrders({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryGetOrders( key.contractAddr,  key.account)).data
				
					
				commit('QUERY', { query: 'GetOrders', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryGetOrders', payload: { options: { all }, params: {...key},query }})
				return getters['getGetOrders']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryGetOrders API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryGetOrderByID({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryGetOrderById( key.contractAddr,  key.priceDenom,  key.assetDenom,  key.id)).data
				
					
				commit('QUERY', { query: 'GetOrderByID', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryGetOrderByID', payload: { options: { all }, params: {...key},query }})
				return getters['getGetOrderByID']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryGetOrderByID API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		
		
		 		
		
		
		async QueryGetHistoricalPrices({ commit, rootGetters, getters }, { options: { subscribe, all} = { subscribe:false, all:false}, params, query=null }) {
			try {
				const key = params ?? {};
				const queryClient=await initQueryClient(rootGetters)
				let value= (await queryClient.queryGetHistoricalPrices( key.contractAddr,  key.priceDenom,  key.assetDenom,  key.periodLengthInSeconds,  key.numOfPeriods)).data
				
					
				commit('QUERY', { query: 'GetHistoricalPrices', key: { params: {...key}, query}, value })
				if (subscribe) commit('SUBSCRIBE', { action: 'QueryGetHistoricalPrices', payload: { options: { all }, params: {...key},query }})
				return getters['getGetHistoricalPrices']( { params: {...key}, query}) ?? {}
			} catch (e) {
				throw new Error('QueryClient:QueryGetHistoricalPrices API Node Unavailable. Could not perform query: ' + e.message)
				
			}
		},
		
		
		async sendMsgRegisterContract({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgRegisterContract(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgRegisterContract:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgRegisterContract:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgLiquidation({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgLiquidation(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgLiquidation:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgLiquidation:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgPlaceOrders({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgPlaceOrders(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgPlaceOrders:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgPlaceOrders:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		async sendMsgCancelOrders({ rootGetters }, { value, fee = [], memo = '' }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgCancelOrders(value)
				const result = await txClient.signAndBroadcast([msg], {fee: { amount: fee, 
	gas: "200000" }, memo})
				return result
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgCancelOrders:Init Could not initialize signing client. Wallet is required.')
				}else{
					throw new Error('TxClient:MsgCancelOrders:Send Could not broadcast Tx: '+ e.message)
				}
			}
		},
		
		async MsgRegisterContract({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgRegisterContract(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgRegisterContract:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgRegisterContract:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgLiquidation({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgLiquidation(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgLiquidation:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgLiquidation:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgPlaceOrders({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgPlaceOrders(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgPlaceOrders:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgPlaceOrders:Create Could not create message: ' + e.message)
				}
			}
		},
		async MsgCancelOrders({ rootGetters }, { value }) {
			try {
				const txClient=await initTxClient(rootGetters)
				const msg = await txClient.msgCancelOrders(value)
				return msg
			} catch (e) {
				if (e == MissingWalletError) {
					throw new Error('TxClient:MsgCancelOrders:Init Could not initialize signing client. Wallet is required.')
				} else{
					throw new Error('TxClient:MsgCancelOrders:Create Could not create message: ' + e.message)
				}
			}
		},
		
	}
}
