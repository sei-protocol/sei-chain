import { txClient, queryClient, MissingWalletError, registry } from './module';
// @ts-ignore
import { SpVuexError } from '@starport/vuex';
import { WeightedVoteOption } from "./module/types/cosmos/gov/v1beta1/gov";
import { TextProposal } from "./module/types/cosmos/gov/v1beta1/gov";
import { Deposit } from "./module/types/cosmos/gov/v1beta1/gov";
import { Proposal } from "./module/types/cosmos/gov/v1beta1/gov";
import { TallyResult } from "./module/types/cosmos/gov/v1beta1/gov";
import { Vote } from "./module/types/cosmos/gov/v1beta1/gov";
import { DepositParams } from "./module/types/cosmos/gov/v1beta1/gov";
import { VotingParams } from "./module/types/cosmos/gov/v1beta1/gov";
import { TallyParams } from "./module/types/cosmos/gov/v1beta1/gov";
export { WeightedVoteOption, TextProposal, Deposit, Proposal, TallyResult, Vote, DepositParams, VotingParams, TallyParams };
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
        Proposal: {},
        Proposals: {},
        Vote: {},
        Votes: {},
        Params: {},
        Deposit: {},
        Deposits: {},
        TallyResult: {},
        _Structure: {
            WeightedVoteOption: getStructure(WeightedVoteOption.fromPartial({})),
            TextProposal: getStructure(TextProposal.fromPartial({})),
            Deposit: getStructure(Deposit.fromPartial({})),
            Proposal: getStructure(Proposal.fromPartial({})),
            TallyResult: getStructure(TallyResult.fromPartial({})),
            Vote: getStructure(Vote.fromPartial({})),
            DepositParams: getStructure(DepositParams.fromPartial({})),
            VotingParams: getStructure(VotingParams.fromPartial({})),
            TallyParams: getStructure(TallyParams.fromPartial({})),
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
        getProposal: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Proposal[JSON.stringify(params)] ?? {};
        },
        getProposals: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Proposals[JSON.stringify(params)] ?? {};
        },
        getVote: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Vote[JSON.stringify(params)] ?? {};
        },
        getVotes: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Votes[JSON.stringify(params)] ?? {};
        },
        getParams: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Params[JSON.stringify(params)] ?? {};
        },
        getDeposit: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Deposit[JSON.stringify(params)] ?? {};
        },
        getDeposits: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.Deposits[JSON.stringify(params)] ?? {};
        },
        getTallyResult: (state) => (params = { params: {} }) => {
            if (!params.query) {
                params.query = null;
            }
            return state.TallyResult[JSON.stringify(params)] ?? {};
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
            console.log('Vuex module: cosmos.gov.v1beta1 initialized!');
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
        async QueryProposal({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryProposal(key.proposal_id)).data;
                commit('QUERY', { query: 'Proposal', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryProposal', payload: { options: { all }, params: { ...key }, query } });
                return getters['getProposal']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryProposal', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryProposals({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryProposals(query)).data;
                while (all && value.pagination && value.pagination.next_key != null) {
                    let next_values = (await queryClient.queryProposals({ ...query, 'pagination.key': value.pagination.next_key })).data;
                    value = mergeResults(value, next_values);
                }
                commit('QUERY', { query: 'Proposals', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryProposals', payload: { options: { all }, params: { ...key }, query } });
                return getters['getProposals']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryProposals', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryVote({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryVote(key.proposal_id, key.voter)).data;
                commit('QUERY', { query: 'Vote', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryVote', payload: { options: { all }, params: { ...key }, query } });
                return getters['getVote']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryVote', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryVotes({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryVotes(key.proposal_id, query)).data;
                while (all && value.pagination && value.pagination.next_key != null) {
                    let next_values = (await queryClient.queryVotes(key.proposal_id, { ...query, 'pagination.key': value.pagination.next_key })).data;
                    value = mergeResults(value, next_values);
                }
                commit('QUERY', { query: 'Votes', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryVotes', payload: { options: { all }, params: { ...key }, query } });
                return getters['getVotes']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryVotes', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryParams({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryParams(key.params_type)).data;
                commit('QUERY', { query: 'Params', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryParams', payload: { options: { all }, params: { ...key }, query } });
                return getters['getParams']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryParams', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryDeposit({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryDeposit(key.proposal_id, key.depositor)).data;
                commit('QUERY', { query: 'Deposit', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryDeposit', payload: { options: { all }, params: { ...key }, query } });
                return getters['getDeposit']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryDeposit', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryDeposits({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryDeposits(key.proposal_id, query)).data;
                while (all && value.pagination && value.pagination.next_key != null) {
                    let next_values = (await queryClient.queryDeposits(key.proposal_id, { ...query, 'pagination.key': value.pagination.next_key })).data;
                    value = mergeResults(value, next_values);
                }
                commit('QUERY', { query: 'Deposits', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryDeposits', payload: { options: { all }, params: { ...key }, query } });
                return getters['getDeposits']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryDeposits', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async QueryTallyResult({ commit, rootGetters, getters }, { options: { subscribe, all } = { subscribe: false, all: false }, params, query = null }) {
            try {
                const key = params ?? {};
                const queryClient = await initQueryClient(rootGetters);
                let value = (await queryClient.queryTallyResult(key.proposal_id)).data;
                commit('QUERY', { query: 'TallyResult', key: { params: { ...key }, query }, value });
                if (subscribe)
                    commit('SUBSCRIBE', { action: 'QueryTallyResult', payload: { options: { all }, params: { ...key }, query } });
                return getters['getTallyResult']({ params: { ...key }, query }) ?? {};
            }
            catch (e) {
                throw new SpVuexError('QueryClient:QueryTallyResult', 'API Node Unavailable. Could not perform query: ' + e.message);
            }
        },
        async sendMsgDeposit({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgDeposit(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgDeposit:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgDeposit:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async sendMsgVoteWeighted({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgVoteWeighted(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgVoteWeighted:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgVoteWeighted:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async sendMsgVote({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgVote(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgVote:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgVote:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async sendMsgSubmitProposal({ rootGetters }, { value, fee = [], memo = '' }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgSubmitProposal(value);
                const result = await txClient.signAndBroadcast([msg], { fee: { amount: fee,
                        gas: "200000" }, memo });
                return result;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgSubmitProposal:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgSubmitProposal:Send', 'Could not broadcast Tx: ' + e.message);
                }
            }
        },
        async MsgDeposit({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgDeposit(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgDeposit:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgDeposit:Create', 'Could not create message: ' + e.message);
                }
            }
        },
        async MsgVoteWeighted({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgVoteWeighted(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgVoteWeighted:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgVoteWeighted:Create', 'Could not create message: ' + e.message);
                }
            }
        },
        async MsgVote({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgVote(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgVote:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgVote:Create', 'Could not create message: ' + e.message);
                }
            }
        },
        async MsgSubmitProposal({ rootGetters }, { value }) {
            try {
                const txClient = await initTxClient(rootGetters);
                const msg = await txClient.msgSubmitProposal(value);
                return msg;
            }
            catch (e) {
                if (e == MissingWalletError) {
                    throw new SpVuexError('TxClient:MsgSubmitProposal:Init', 'Could not initialize signing client. Wallet is required.');
                }
                else {
                    throw new SpVuexError('TxClient:MsgSubmitProposal:Create', 'Could not create message: ' + e.message);
                }
            }
        },
    }
};
