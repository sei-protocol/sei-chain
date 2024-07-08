import { OfflineSigner } from "@cosmjs/proto-signing";

export interface Env {
  apiURL: string
  rpcURL: string
  prefix?: string
}