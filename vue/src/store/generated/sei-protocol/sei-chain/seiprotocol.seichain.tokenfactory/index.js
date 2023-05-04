import { txClient, queryClient, MissingWalletError, registry } from './module';
// @ts-ignore
import { SpVuexError } from '@starport/vuex';
import { DenomAuthorityMetadata } from "./module/types/tokenfactory/authorityMetadata";
import { GenesisDenom } from "./module/types/tokenfactory/genesis";
import { AddCreatorsToDenomFeeWhitelistProposal } from "./module/types/tokenfactory/gov";
import { Params } from "./module/types/tokenfactory/params";
export { DenomAuthorityMetadata, GenesisDenom, AddCreatorsToDenomFeeWhitelistProposal, Params };
async function initTxClient(vuexGetters) {
    return await txClient(vuexGetters['common/wallet/signer'], {
        addr: vuexGetters['common/env/apiTendermint']
    });
}
async function initQueryClient(vuexGetters) {
    return await queryClient({
        addr: vuexGetters['common/env/apiCosmos']
    });
}
function mergeResults(value, next_values) {
    for (let prop of Object.keys(next_values)) {
        if (Array.isArray(next_values[prop])) {
            value[prop] = [...value[prop], ...next_values[prop]];
        }
        else {
            value[prop] = next_values[prop];
        }
    }
    return value;
}
function getStructure(template) {
    let structure = { fields: [] };
    for (const [key, value] of Object.entries(template)) {
        let field = {};
        field.name = key;
        field.type = typeof value;
        structure.fields.push(field);
    }
    return structure;
}
const getDefaultState = () => {
    return {
        Params: {},
        DenomAuthorityMetadata: {},
        DenomsFromCreator: {},
        DenomCreationFeeWhitelist: {},
        CreatorInDenomFeeWhitelist: {},
        _Structure: {
            DenomAuthorityMetadata: getStructure(DenomAuthorityMetadata.fromPartial({})),
            GenesisDenom: getStructure(GenesisDenom.fromPartial({})),
            AddCreatorsToDenomFeeWhitelistProposal: getStructure(AddCreatorsToDenomFeeWhitelistProposal.fromPartial({})),
            Params: getStructure(Params.fromPartial({})),
        },
        _Registry: registry,
        _Subscriptions: new Set(),
    };
};
// initial state
const state = getDefaultState();
export default {
    namespaced: true,
    state,
    mutations: {
        RESET_STATE(state) {
            Object.assign(state, getDefaultState());
        },
        QUERY(state, { query, key, value }) {
            state[query][JSON.stringify(key)] = value;
        },
        SUBSCRIBE(state, subscription) {
            state._Subscriptions.add(JSON.stringify(subscription));
        },
        UNSUBSCRIBE(state, subscription) {
            state._Subscriptions.delete(JSON.stringify(subscription));
        }
    },
    getters: {
        getParams: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Params[JSON.stringify(params)] ?? {};
        },
        getDenomAuthorityMetadata: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.DenomAuthorityMetadata[JSON.stringify(params)] ?? {};
        },
        getDenomsFromCreator: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.DenomsFromCreator[JSON.stringify(params)] ?? {};
        },
        getDenomCreationFeeWhitelist: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.DenomCreationFeeWhitelist[JSON.stringify(params)] ?? {};
        },
        getCreatorInDenomFeeWhitelist: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.CreatorInDenomFeeWhitelist[JSON.stringify(params)] ?? {};
        },
        getTypeStructure: (state) => (type) => {
            return state._Structure[type].fields;
        },
        getRegistry: (state) => {
            return state._Registry;
        }
    },
    actions: {
        init({ dispatch, rootGetters }) {
            console.log('Vuex module: seiprotocol.seichain.tokenfactory initialized!');
            if (rootGetters['common/env/client']) {
                rootGetters['common/env/client'].on('newblock', () => {
                    dispatch('StoreUpdate');
                });
            }
        },
        resetState({ commit }) {
            commit('RESET_STATE');
        },
        unsubscribe({ commit }, subscription) {
            commit('UNSUBSCRIBE', subscription);
        },
        async StoreUpdate({ state, dispatch }) {
            state._Subscriptions.forEach(async (subscription) => {
                try {
                    const sub = JSON.parse(subscription);
                    await dispatch(sub.action, sub.payload);
                }
                catch (e) {
                    throw new SpVuexError('Subscriptions: ' + e.message);
                }
            });
        },
        async QueryParams({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryParams()).data;
                commit('QUERY', { query: 'Params', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryParams', payload: { options: { all }, params: { ...key }, query } });
                return getters['getParams']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryParams', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryDenomAuthorityMetadata({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryDenomAuthorityMetadata(key.denom)).data;
                commit('QUERY', { query: 'DenomAuthorityMetadata', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryDenomAuthorityMetadata', payload: { options: { all }, params: { ...key }, query } });
                return getters['getDenomAuthorityMetadata']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryDenomAuthorityMetadata', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryDenomsFromCreator({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryDenomsFromCreator(key.creator)).data;
                commit('QUERY', { query: 'DenomsFromCreator', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryDenomsFromCreator', payload: { options: { all }, params: { ...key }, query } });
                return getters['getDenomsFromCreator']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryDenomsFromCreator', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryDenomCreationFeeWhitelist({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryDenomCreationFeeWhitelist()).data;
                commit('QUERY', { query: 'DenomCreationFeeWhitelist', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryDenomCreationFeeWhitelist', payload: { options: { all }, params: { ...key }, query } });
                return getters['getDenomCreationFeeWhitelist']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryDenomCreationFeeWhitelist', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryCreatorInDenomFeeWhitelist({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryCreatorInDenomFeeWhitelist(key.creator)).data;
                commit('QUERY', { query: 'CreatorInDenomFeeWhitelist', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryCreatorInDenomFeeWhitelist', payload: { options: { all }, params: { ...key }, query } });
                return getters['getCreatorInDenomFeeWhitelist']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryCreatorInDenomFeeWhitelist', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async sendMsgChangeAdmin({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgChangeAdmin(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgChangeAdmin:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgChangeAdmin:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async sendMsgBurn({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgBurn(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgBurn:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgBurn:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async sendMsgMint({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgMint(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgMint:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgMint:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async sendMsgCreateDenom({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgCreateDenom(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgCreateDenom:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgCreateDenom:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async MsgChangeAdmin({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgChangeAdmin(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgChangeAdmin:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgChangeAdmin:Create', 'Could not create message: ' + e.message);
                }
            }
        },
        async MsgBurn({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgBurn(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgBurn:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgBurn:Create', 'Could not create message: ' + e.message);
                }
            }
        },
        async MsgMint({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgMint(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgMint:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgMint:Create', 'Could not create message: ' + e.message);
                }
            }
        },
        async MsgCreateDenom({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgCreateDenom(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgCreateDenom:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgCreateDenom:Create', 'Could not create message: ' + e.message);
                }
            }
        },
    }
};
