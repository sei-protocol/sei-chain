import { GeneratedType, OfflineSigner, EncodeObject, Registry } from "@cosmjs/proto-signing";
import { StdFee } from "@cosmjs/launchpad";
import { SigningStargateClient } from "@cosmjs/stargate";
import { Env } from "./env";
import { UnionToIntersection, Return, Constructor } from "./helpers";
import { Module } from "./modules";

const defaultFee = {
  amount: [],
  gas: "200000",
};

export class IgniteClient {
	static plugins: Module[] = [];
  env: Env;
  signer: OfflineSigner;
  registry: Array<[string, GeneratedType]> = [];
  static plugin<T extends Module | Module[]>(plugin: T) {
    const currentPlugins = this.plugins;

    class AugmentedClient extends this {
      static plugins = currentPlugins.concat(plugin);
    }

    if (Array.isArray(plugin)) {
      type Extension = UnionToIntersection<Return<T>['module']>
      return AugmentedClient as typeof AugmentedClient & Constructor<Extension>;  
    }

    type Extension = Return<T>['module']
    return AugmentedClient as typeof AugmentedClient & Constructor<Extension>;
  }
  async signAndBroadcast(msgs: EncodeObject[], fee: StdFee, memo: string) {
    const { address } = (await this.signer.getAccounts())[0]; 
    const signingClient = await SigningStargateClient.connectWithSigner(this.env.rpcURL, this.signer, {registry: new Registry(this.registry), prefix: this.env.prefix});
		return await signingClient.signAndBroadcast(address, msgs, fee ? fee : defaultFee, memo)
  }
  constructor(env: Env, signer: OfflineSigner) {
    this.env = env;
    this.signer = signer;
    const classConstructor = this.constructor as typeof IgniteClient;
    classConstructor.plugins.forEach(plugin => {
      const pluginInstance = plugin(this);
      Object.assign(this, pluginInstance.module)
      if (this.registry) {
        this.registry = this.registry.concat(pluginInstance.registry)
      }
		});
		
  }
}