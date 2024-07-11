"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || function (mod) {
    if (mod && mod.__esModule) return mod;
    var result = {};
    if (mod != null) for (var k in mod) if (k !== "default" && Object.prototype.hasOwnProperty.call(mod, k)) __createBinding(result, mod, k);
    __setModuleDefault(result, mod);
    return result;
};
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
exports.waitFor = exports.fundSeiWallet = exports.fundEvmWallet = void 0;
const util = __importStar(require("node:util"));
const exec = util.promisify(require('node:child_process').exec);
const proto_signing_1 = require("@cosmjs/proto-signing");
function fundEvmWallet(receiverWallet, rpc) {
    return __awaiter(this, void 0, void 0, function* () {
        const { stdout } = yield exec('seid keys show admin --address');
        const address = yield receiverWallet.getAddress();
        yield exec(`seid tx evm send ${address} 10000000000000000000000 --from admin --fees 24000use --evm-rpc=${rpc}`);
        console.log('Funded on evm');
    });
}
exports.fundEvmWallet = fundEvmWallet;
function fundSeiWallet(receiverWallet) {
    return __awaiter(this, void 0, void 0, function* () {
        let address;
        if (receiverWallet instanceof proto_signing_1.DirectSecp256k1HdWallet) {
            const [accountData, _] = yield receiverWallet.getAccounts();
            address = accountData.address;
        }
        else {
            address = receiverWallet;
        }
        const { stdout } = yield exec('seid keys show admin --address');
        const senderAddress = stdout.trim().replaceAll(' ', '');
        console.log('Funding sei address');
        yield exec(`seid tx bank send ${senderAddress} ${address} 10000000000usei --from admin --fees 24200usei -y`);
    });
}
exports.fundSeiWallet = fundSeiWallet;
function waitFor(seconds) {
    return __awaiter(this, void 0, void 0, function* () {
        return new Promise((resolve) => {
            return setTimeout(() => {
                resolve();
            }, seconds * 1000);
        });
    });
}
exports.waitFor = waitFor;
