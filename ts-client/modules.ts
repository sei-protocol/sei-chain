import { IgniteClient } from "./client";
import { GeneratedType } from "@cosmjs/proto-signing";

export type ModuleInterface = { [key: string]: any }
export type Module = (instance: IgniteClient) => { module: ModuleInterface, registry: [string, GeneratedType][] }
