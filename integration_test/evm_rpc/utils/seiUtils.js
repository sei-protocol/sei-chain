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
exports.sendFundsFromSeiClient = exports.generateSeiAddressFromMnemonic = exports.generateEvmAddressFromMnemonic = exports.signMessage = exports.associateWallet = exports.createSeiWallet = exports.createSeiProvider = void 0;
const crypto_1 = require("@cosmjs/crypto");
const proto_signing_1 = require("@cosmjs/proto-signing");
const stargate_1 = require("@cosmjs/stargate");
const ethers_1 = require("ethers");
const secp256k1_1 = require("@noble/curves/secp256k1");
const viem_1 = require("viem");
const cmdUtils_1 = require("./cmdUtils");
function createSeiProvider(rpcUrl, wallet) {
    return __awaiter(this, void 0, void 0, function* () {
        return yield stargate_1.SigningStargateClient.connectWithSigner(rpcUrl, wallet);
    });
}
exports.createSeiProvider = createSeiProvider;
function createSeiWallet() {
    return __awaiter(this, void 0, void 0, function* () {
        return yield proto_signing_1.DirectSecp256k1HdWallet.generate(24, {
            prefix: 'sei'
        });
    });
}
exports.createSeiWallet = createSeiWallet;
function associateWallet(evmProvider, evmWallet) {
    return __awaiter(this, void 0, void 0, function* () {
        const message = "account association";
        const signature = yield evmWallet.signMessage(message);
        const { r, s } = secp256k1_1.secp256k1.Signature.fromCompact(signature.slice(2, 130));
        const v = (0, viem_1.hexToNumber)(`0x${signature.slice(130)}`);
        const messageLength = Buffer.from(message, "utf8").length;
        const messageToSign = `\x19Ethereum Signed Message:\n${messageLength}${message}`;
        const request = {
            r: (0, viem_1.numberToHex)(r),
            s: (0, viem_1.numberToHex)(s),
            v: (0, viem_1.numberToHex)(v - 27),
            custom_message: messageToSign,
        };
        yield evmProvider.send('sei_associate', [request]);
        yield (0, cmdUtils_1.waitFor)(2);
    });
}
exports.associateWallet = associateWallet;
function signMessage(evmWallet) {
    return __awaiter(this, void 0, void 0, function* () {
        const customMessage = 'associate wallets';
        const sign = yield evmWallet.signMessage(customMessage);
        const values = ethers_1.ethers.Signature.from(sign);
        const { r, v, s } = values;
        return { r, v, s };
    });
}
exports.signMessage = signMessage;
function generateEvmAddressFromMnemonic(seiWallet) {
    return __awaiter(this, void 0, void 0, function* () {
        const evmWallet = ethers_1.ethers.HDNodeWallet.fromPhrase(seiWallet.mnemonic, '', 'm/44\'/118\'/0\'/0/0');
        return yield evmWallet.getAddress();
    });
}
exports.generateEvmAddressFromMnemonic = generateEvmAddressFromMnemonic;
function generateSeiAddressFromMnemonic(evmWallet) {
    return __awaiter(this, void 0, void 0, function* () {
        const mnemonic = evmWallet.mnemonic.phrase;
        const wallet = yield proto_signing_1.DirectSecp256k1HdWallet.fromMnemonic(mnemonic, {
            prefix: 'sei',
            hdPaths: [(0, crypto_1.stringToPath)('m/44\'/60\'/0\'/0/0')]
        });
        return (yield wallet.getAccounts())[0].address;
    });
}
exports.generateSeiAddressFromMnemonic = generateSeiAddressFromMnemonic;
function sendFundsFromSeiClient(signingClient, senderWallet, receiverAddress) {
    return __awaiter(this, void 0, void 0, function* () {
        const fee = {
            amount: (0, proto_signing_1.coins)(24000, "usei"), // fee amount
            gas: "250000", // gas limit
        };
        const transferAmount = (0, stargate_1.coin)('100000', 'usei');
        const receipt = yield signingClient.sendTokens(senderWallet, receiverAddress, [transferAmount], fee);
        return receipt.transactionHash;
    });
}
exports.sendFundsFromSeiClient = sendFundsFromSeiClient;
