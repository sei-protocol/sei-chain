"use strict";
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.sendFundsFromEvmClient = exports.createEvmWallet = exports.createEvmProvider = void 0;
const ethers_1 = require("ethers");
function createEvmProvider(rpcUrl) {
    return __awaiter(this, void 0, void 0, function* () {
        return new ethers_1.ethers.JsonRpcProvider(rpcUrl);
    });
}
exports.createEvmProvider = createEvmProvider;
function createEvmWallet(evmClient) {
    return __awaiter(this, void 0, void 0, function* () {
        return ethers_1.ethers.Wallet.createRandom(evmClient);
    });
}
exports.createEvmWallet = createEvmWallet;
function sendFundsFromEvmClient(wallet, recipientAddress) {
    return __awaiter(this, void 0, void 0, function* () {
        const tx = {
            to: recipientAddress,
            value: ethers_1.ethers.parseUnits('0.1', 'ether'),
        };
        // Send the transaction
        const txResponse = yield wallet.sendTransaction(tx);
        // Wait for the transaction to be mined
        const receipt = yield txResponse.wait();
        return [receipt === null || receipt === void 0 ? void 0 : receipt.hash, receipt === null || receipt === void 0 ? void 0 : receipt.blockNumber, receipt === null || receipt === void 0 ? void 0 : receipt.blockHash];
    });
}
exports.sendFundsFromEvmClient = sendFundsFromEvmClient;
