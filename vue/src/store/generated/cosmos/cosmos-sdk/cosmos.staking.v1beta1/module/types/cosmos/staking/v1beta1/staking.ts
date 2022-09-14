/* eslint-disable */
import { Timestamp } from "../../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Header } from "../../../tendermint/types/types";
import { Any } from "../../../google/protobuf/any";
import { Duration } from "../../../google/protobuf/duration";
import { Coin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmos.staking.v1beta1";

/** BondStatus is the status of a validator. */
export enum BondStatus {
  /** BOND_STATUS_UNSPECIFIED - UNSPECIFIED defines an invalid validator status. */
  BOND_STATUS_UNSPECIFIED = 0,
  /** BOND_STATUS_UNBONDED - UNBONDED defines a validator that is not bonded. */
  BOND_STATUS_UNBONDED = 1,
  /** BOND_STATUS_UNBONDING - UNBONDING defines a validator that is unbonding. */
  BOND_STATUS_UNBONDING = 2,
  /** BOND_STATUS_BONDED - BONDED defines a validator that is bonded. */
  BOND_STATUS_BONDED = 3,
  UNRECOGNIZED = -1,
}

export function bondStatusFromJSON(object: any): BondStatus {
  switch (object) {
    case 0:
    case "BOND_STATUS_UNSPECIFIED":
      return BondStatus.BOND_STATUS_UNSPECIFIED;
    case 1:
    case "BOND_STATUS_UNBONDED":
      return BondStatus.BOND_STATUS_UNBONDED;
    case 2:
    case "BOND_STATUS_UNBONDING":
      return BondStatus.BOND_STATUS_UNBONDING;
    case 3:
    case "BOND_STATUS_BONDED":
      return BondStatus.BOND_STATUS_BONDED;
    case -1:
    case "UNRECOGNIZED":
    default:
      return BondStatus.UNRECOGNIZED;
  }
}

export function bondStatusToJSON(object: BondStatus): string {
  switch (object) {
    case BondStatus.BOND_STATUS_UNSPECIFIED:
      return "BOND_STATUS_UNSPECIFIED";
    case BondStatus.BOND_STATUS_UNBONDED:
      return "BOND_STATUS_UNBONDED";
    case BondStatus.BOND_STATUS_UNBONDING:
      return "BOND_STATUS_UNBONDING";
    case BondStatus.BOND_STATUS_BONDED:
      return "BOND_STATUS_BONDED";
    default:
      return "UNKNOWN";
  }
}

/**
 * HistoricalInfo contains header and validator information for a given block.
 * It is stored as part of staking module's state, which persists the `n` most
 * recent HistoricalInfo
 * (`n` is set by the staking module's `historical_entries` parameter).
 */
export interface HistoricalInfo {
  header: Header | undefined;
  valset: Validator[];
}

/**
 * CommissionRates defines the initial commission rates to be used for creating
 * a validator.
 */
export interface CommissionRates {
  /** rate is the commission rate charged to delegators, as a fraction. */
  rate: string;
  /** max_rate defines the maximum commission rate which validator can ever charge, as a fraction. */
  max_rate: string;
  /** max_change_rate defines the maximum daily increase of the validator commission, as a fraction. */
  max_change_rate: string;
}

/** Commission defines commission parameters for a given validator. */
export interface Commission {
  /** commission_rates defines the initial commission rates to be used for creating a validator. */
  commission_rates: CommissionRates | undefined;
  /** update_time is the last time the commission rate was changed. */
  update_time: Date | undefined;
}

/** Description defines a validator description. */
export interface Description {
  /** moniker defines a human-readable name for the validator. */
  moniker: string;
  /** identity defines an optional identity signature (ex. UPort or Keybase). */
  identity: string;
  /** website defines an optional website link. */
  website: string;
  /** security_contact defines an optional email for security contact. */
  security_contact: string;
  /** details define other optional details. */
  details: string;
}

/**
 * Validator defines a validator, together with the total amount of the
 * Validator's bond shares and their exchange rate to coins. Slashing results in
 * a decrease in the exchange rate, allowing correct calculation of future
 * undelegations without iterating over delegators. When coins are delegated to
 * this validator, the validator is credited with a delegation whose number of
 * bond shares is based on the amount of coins delegated divided by the current
 * exchange rate. Voting power can be calculated as total bonded shares
 * multiplied by exchange rate.
 */
export interface Validator {
  /** operator_address defines the address of the validator's operator; bech encoded in JSON. */
  operator_address: string;
  /** consensus_pubkey is the consensus public key of the validator, as a Protobuf Any. */
  consensus_pubkey: Any | undefined;
  /** jailed defined whether the validator has been jailed from bonded status or not. */
  jailed: boolean;
  /** status is the validator status (bonded/unbonding/unbonded). */
  status: BondStatus;
  /** tokens define the delegated tokens (incl. self-delegation). */
  tokens: string;
  /** delegator_shares defines total shares issued to a validator's delegators. */
  delegator_shares: string;
  /** description defines the description terms for the validator. */
  description: Description | undefined;
  /** unbonding_height defines, if unbonding, the height at which this validator has begun unbonding. */
  unbonding_height: number;
  /** unbonding_time defines, if unbonding, the min time for the validator to complete unbonding. */
  unbonding_time: Date | undefined;
  /** commission defines the commission parameters. */
  commission: Commission | undefined;
  /** min_self_delegation is the validator's self declared minimum self delegation. */
  min_self_delegation: string;
}

/** ValAddresses defines a repeated set of validator addresses. */
export interface ValAddresses {
  addresses: string[];
}

/**
 * DVPair is struct that just has a delegator-validator pair with no other data.
 * It is intended to be used as a marshalable pointer. For example, a DVPair can
 * be used to construct the key to getting an UnbondingDelegation from state.
 */
export interface DVPair {
  delegator_address: string;
  validator_address: string;
}

/** DVPairs defines an array of DVPair objects. */
export interface DVPairs {
  pairs: DVPair[];
}

/**
 * DVVTriplet is struct that just has a delegator-validator-validator triplet
 * with no other data. It is intended to be used as a marshalable pointer. For
 * example, a DVVTriplet can be used to construct the key to getting a
 * Redelegation from state.
 */
export interface DVVTriplet {
  delegator_address: string;
  validator_src_address: string;
  validator_dst_address: string;
}

/** DVVTriplets defines an array of DVVTriplet objects. */
export interface DVVTriplets {
  triplets: DVVTriplet[];
}

/**
 * Delegation represents the bond with tokens held by an account. It is
 * owned by one delegator, and is associated with the voting power of one
 * validator.
 */
export interface Delegation {
  /** delegator_address is the bech32-encoded address of the delegator. */
  delegator_address: string;
  /** validator_address is the bech32-encoded address of the validator. */
  validator_address: string;
  /** shares define the delegation shares received. */
  shares: string;
}

/**
 * UnbondingDelegation stores all of a single delegator's unbonding bonds
 * for a single validator in an time-ordered list.
 */
export interface UnbondingDelegation {
  /** delegator_address is the bech32-encoded address of the delegator. */
  delegator_address: string;
  /** validator_address is the bech32-encoded address of the validator. */
  validator_address: string;
  /** entries are the unbonding delegation entries. */
  entries: UnbondingDelegationEntry[];
}

/** UnbondingDelegationEntry defines an unbonding object with relevant metadata. */
export interface UnbondingDelegationEntry {
  /** creation_height is the height which the unbonding took place. */
  creation_height: number;
  /** completion_time is the unix time for unbonding completion. */
  completion_time: Date | undefined;
  /** initial_balance defines the tokens initially scheduled to receive at completion. */
  initial_balance: string;
  /** balance defines the tokens to receive at completion. */
  balance: string;
}

/** RedelegationEntry defines a redelegation object with relevant metadata. */
export interface RedelegationEntry {
  /** creation_height  defines the height which the redelegation took place. */
  creation_height: number;
  /** completion_time defines the unix time for redelegation completion. */
  completion_time: Date | undefined;
  /** initial_balance defines the initial balance when redelegation started. */
  initial_balance: string;
  /** shares_dst is the amount of destination-validator shares created by redelegation. */
  shares_dst: string;
}

/**
 * Redelegation contains the list of a particular delegator's redelegating bonds
 * from a particular source validator to a particular destination validator.
 */
export interface Redelegation {
  /** delegator_address is the bech32-encoded address of the delegator. */
  delegator_address: string;
  /** validator_src_address is the validator redelegation source operator address. */
  validator_src_address: string;
  /** validator_dst_address is the validator redelegation destination operator address. */
  validator_dst_address: string;
  /** entries are the redelegation entries. */
  entries: RedelegationEntry[];
}

/** Params defines the parameters for the staking module. */
export interface Params {
  /** unbonding_time is the time duration of unbonding. */
  unbonding_time: Duration | undefined;
  /** max_validators is the maximum number of validators. */
  max_validators: number;
  /** max_entries is the max entries for either unbonding delegation or redelegation (per pair/trio). */
  max_entries: number;
  /** historical_entries is the number of historical entries to persist. */
  historical_entries: number;
  /** bond_denom defines the bondable coin denomination. */
  bond_denom: string;
}

/**
 * DelegationResponse is equivalent to Delegation except that it contains a
 * balance in addition to shares which is more suitable for client responses.
 */
export interface DelegationResponse {
  delegation: Delegation | undefined;
  balance: Coin | undefined;
}

/**
 * RedelegationEntryResponse is equivalent to a RedelegationEntry except that it
 * contains a balance in addition to shares which is more suitable for client
 * responses.
 */
export interface RedelegationEntryResponse {
  redelegation_entry: RedelegationEntry | undefined;
  balance: string;
}

/**
 * RedelegationResponse is equivalent to a Redelegation except that its entries
 * contain a balance in addition to shares which is more suitable for client
 * responses.
 */
export interface RedelegationResponse {
  redelegation: Redelegation | undefined;
  entries: RedelegationEntryResponse[];
}

/**
 * Pool is used for tracking bonded and not-bonded token supply of the bond
 * denomination.
 */
export interface Pool {
  not_bonded_tokens: string;
  bonded_tokens: string;
}

const baseHistoricalInfo: object = {};

export const HistoricalInfo = {
  encode(message: HistoricalInfo, writer: Writer = Writer.create()): Writer {
    if (message.header !== undefined) {
      Header.encode(message.header, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.valset) {
      Validator.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): HistoricalInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseHistoricalInfo } as HistoricalInfo;
    message.valset = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.header = Header.decode(reader, reader.uint32());
          break;
        case 2:
          message.valset.push(Validator.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): HistoricalInfo {
    const message = { ...baseHistoricalInfo } as HistoricalInfo;
    message.valset = [];
    if (object.header !== undefined && object.header !== null) {
      message.header = Header.fromJSON(object.header);
    } else {
      message.header = undefined;
    }
    if (object.valset !== undefined && object.valset !== null) {
      for (const e of object.valset) {
        message.valset.push(Validator.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: HistoricalInfo): unknown {
    const obj: any = {};
    message.header !== undefined &&
      (obj.header = message.header ? Header.toJSON(message.header) : undefined);
    if (message.valset) {
      obj.valset = message.valset.map((e) =>
        e ? Validator.toJSON(e) : undefined
      );
    } else {
      obj.valset = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<HistoricalInfo>): HistoricalInfo {
    const message = { ...baseHistoricalInfo } as HistoricalInfo;
    message.valset = [];
    if (object.header !== undefined && object.header !== null) {
      message.header = Header.fromPartial(object.header);
    } else {
      message.header = undefined;
    }
    if (object.valset !== undefined && object.valset !== null) {
      for (const e of object.valset) {
        message.valset.push(Validator.fromPartial(e));
      }
    }
    return message;
  },
};

const baseCommissionRates: object = {
  rate: "",
  max_rate: "",
  max_change_rate: "",
};

export const CommissionRates = {
  encode(message: CommissionRates, writer: Writer = Writer.create()): Writer {
    if (message.rate !== "") {
      writer.uint32(10).string(message.rate);
    }
    if (message.max_rate !== "") {
      writer.uint32(18).string(message.max_rate);
    }
    if (message.max_change_rate !== "") {
      writer.uint32(26).string(message.max_change_rate);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): CommissionRates {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCommissionRates } as CommissionRates;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.rate = reader.string();
          break;
        case 2:
          message.max_rate = reader.string();
          break;
        case 3:
          message.max_change_rate = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): CommissionRates {
    const message = { ...baseCommissionRates } as CommissionRates;
    if (object.rate !== undefined && object.rate !== null) {
      message.rate = String(object.rate);
    } else {
      message.rate = "";
    }
    if (object.max_rate !== undefined && object.max_rate !== null) {
      message.max_rate = String(object.max_rate);
    } else {
      message.max_rate = "";
    }
    if (
      object.max_change_rate !== undefined &&
      object.max_change_rate !== null
    ) {
      message.max_change_rate = String(object.max_change_rate);
    } else {
      message.max_change_rate = "";
    }
    return message;
  },

  toJSON(message: CommissionRates): unknown {
    const obj: any = {};
    message.rate !== undefined && (obj.rate = message.rate);
    message.max_rate !== undefined && (obj.max_rate = message.max_rate);
    message.max_change_rate !== undefined &&
      (obj.max_change_rate = message.max_change_rate);
    return obj;
  },

  fromPartial(object: DeepPartial<CommissionRates>): CommissionRates {
    const message = { ...baseCommissionRates } as CommissionRates;
    if (object.rate !== undefined && object.rate !== null) {
      message.rate = object.rate;
    } else {
      message.rate = "";
    }
    if (object.max_rate !== undefined && object.max_rate !== null) {
      message.max_rate = object.max_rate;
    } else {
      message.max_rate = "";
    }
    if (
      object.max_change_rate !== undefined &&
      object.max_change_rate !== null
    ) {
      message.max_change_rate = object.max_change_rate;
    } else {
      message.max_change_rate = "";
    }
    return message;
  },
};

const baseCommission: object = {};

export const Commission = {
  encode(message: Commission, writer: Writer = Writer.create()): Writer {
    if (message.commission_rates !== undefined) {
      CommissionRates.encode(
        message.commission_rates,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.update_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.update_time),
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Commission {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCommission } as Commission;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.commission_rates = CommissionRates.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.update_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Commission {
    const message = { ...baseCommission } as Commission;
    if (
      object.commission_rates !== undefined &&
      object.commission_rates !== null
    ) {
      message.commission_rates = CommissionRates.fromJSON(
        object.commission_rates
      );
    } else {
      message.commission_rates = undefined;
    }
    if (object.update_time !== undefined && object.update_time !== null) {
      message.update_time = fromJsonTimestamp(object.update_time);
    } else {
      message.update_time = undefined;
    }
    return message;
  },

  toJSON(message: Commission): unknown {
    const obj: any = {};
    message.commission_rates !== undefined &&
      (obj.commission_rates = message.commission_rates
        ? CommissionRates.toJSON(message.commission_rates)
        : undefined);
    message.update_time !== undefined &&
      (obj.update_time =
        message.update_time !== undefined
          ? message.update_time.toISOString()
          : null);
    return obj;
  },

  fromPartial(object: DeepPartial<Commission>): Commission {
    const message = { ...baseCommission } as Commission;
    if (
      object.commission_rates !== undefined &&
      object.commission_rates !== null
    ) {
      message.commission_rates = CommissionRates.fromPartial(
        object.commission_rates
      );
    } else {
      message.commission_rates = undefined;
    }
    if (object.update_time !== undefined && object.update_time !== null) {
      message.update_time = object.update_time;
    } else {
      message.update_time = undefined;
    }
    return message;
  },
};

const baseDescription: object = {
  moniker: "",
  identity: "",
  website: "",
  security_contact: "",
  details: "",
};

export const Description = {
  encode(message: Description, writer: Writer = Writer.create()): Writer {
    if (message.moniker !== "") {
      writer.uint32(10).string(message.moniker);
    }
    if (message.identity !== "") {
      writer.uint32(18).string(message.identity);
    }
    if (message.website !== "") {
      writer.uint32(26).string(message.website);
    }
    if (message.security_contact !== "") {
      writer.uint32(34).string(message.security_contact);
    }
    if (message.details !== "") {
      writer.uint32(42).string(message.details);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Description {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDescription } as Description;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.moniker = reader.string();
          break;
        case 2:
          message.identity = reader.string();
          break;
        case 3:
          message.website = reader.string();
          break;
        case 4:
          message.security_contact = reader.string();
          break;
        case 5:
          message.details = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Description {
    const message = { ...baseDescription } as Description;
    if (object.moniker !== undefined && object.moniker !== null) {
      message.moniker = String(object.moniker);
    } else {
      message.moniker = "";
    }
    if (object.identity !== undefined && object.identity !== null) {
      message.identity = String(object.identity);
    } else {
      message.identity = "";
    }
    if (object.website !== undefined && object.website !== null) {
      message.website = String(object.website);
    } else {
      message.website = "";
    }
    if (
      object.security_contact !== undefined &&
      object.security_contact !== null
    ) {
      message.security_contact = String(object.security_contact);
    } else {
      message.security_contact = "";
    }
    if (object.details !== undefined && object.details !== null) {
      message.details = String(object.details);
    } else {
      message.details = "";
    }
    return message;
  },

  toJSON(message: Description): unknown {
    const obj: any = {};
    message.moniker !== undefined && (obj.moniker = message.moniker);
    message.identity !== undefined && (obj.identity = message.identity);
    message.website !== undefined && (obj.website = message.website);
    message.security_contact !== undefined &&
      (obj.security_contact = message.security_contact);
    message.details !== undefined && (obj.details = message.details);
    return obj;
  },

  fromPartial(object: DeepPartial<Description>): Description {
    const message = { ...baseDescription } as Description;
    if (object.moniker !== undefined && object.moniker !== null) {
      message.moniker = object.moniker;
    } else {
      message.moniker = "";
    }
    if (object.identity !== undefined && object.identity !== null) {
      message.identity = object.identity;
    } else {
      message.identity = "";
    }
    if (object.website !== undefined && object.website !== null) {
      message.website = object.website;
    } else {
      message.website = "";
    }
    if (
      object.security_contact !== undefined &&
      object.security_contact !== null
    ) {
      message.security_contact = object.security_contact;
    } else {
      message.security_contact = "";
    }
    if (object.details !== undefined && object.details !== null) {
      message.details = object.details;
    } else {
      message.details = "";
    }
    return message;
  },
};

const baseValidator: object = {
  operator_address: "",
  jailed: false,
  status: 0,
  tokens: "",
  delegator_shares: "",
  unbonding_height: 0,
  min_self_delegation: "",
};

export const Validator = {
  encode(message: Validator, writer: Writer = Writer.create()): Writer {
    if (message.operator_address !== "") {
      writer.uint32(10).string(message.operator_address);
    }
    if (message.consensus_pubkey !== undefined) {
      Any.encode(message.consensus_pubkey, writer.uint32(18).fork()).ldelim();
    }
    if (message.jailed === true) {
      writer.uint32(24).bool(message.jailed);
    }
    if (message.status !== 0) {
      writer.uint32(32).int32(message.status);
    }
    if (message.tokens !== "") {
      writer.uint32(42).string(message.tokens);
    }
    if (message.delegator_shares !== "") {
      writer.uint32(50).string(message.delegator_shares);
    }
    if (message.description !== undefined) {
      Description.encode(
        message.description,
        writer.uint32(58).fork()
      ).ldelim();
    }
    if (message.unbonding_height !== 0) {
      writer.uint32(64).int64(message.unbonding_height);
    }
    if (message.unbonding_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.unbonding_time),
        writer.uint32(74).fork()
      ).ldelim();
    }
    if (message.commission !== undefined) {
      Commission.encode(message.commission, writer.uint32(82).fork()).ldelim();
    }
    if (message.min_self_delegation !== "") {
      writer.uint32(90).string(message.min_self_delegation);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Validator {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidator } as Validator;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.operator_address = reader.string();
          break;
        case 2:
          message.consensus_pubkey = Any.decode(reader, reader.uint32());
          break;
        case 3:
          message.jailed = reader.bool();
          break;
        case 4:
          message.status = reader.int32() as any;
          break;
        case 5:
          message.tokens = reader.string();
          break;
        case 6:
          message.delegator_shares = reader.string();
          break;
        case 7:
          message.description = Description.decode(reader, reader.uint32());
          break;
        case 8:
          message.unbonding_height = longToNumber(reader.int64() as Long);
          break;
        case 9:
          message.unbonding_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 10:
          message.commission = Commission.decode(reader, reader.uint32());
          break;
        case 11:
          message.min_self_delegation = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Validator {
    const message = { ...baseValidator } as Validator;
    if (
      object.operator_address !== undefined &&
      object.operator_address !== null
    ) {
      message.operator_address = String(object.operator_address);
    } else {
      message.operator_address = "";
    }
    if (
      object.consensus_pubkey !== undefined &&
      object.consensus_pubkey !== null
    ) {
      message.consensus_pubkey = Any.fromJSON(object.consensus_pubkey);
    } else {
      message.consensus_pubkey = undefined;
    }
    if (object.jailed !== undefined && object.jailed !== null) {
      message.jailed = Boolean(object.jailed);
    } else {
      message.jailed = false;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = bondStatusFromJSON(object.status);
    } else {
      message.status = 0;
    }
    if (object.tokens !== undefined && object.tokens !== null) {
      message.tokens = String(object.tokens);
    } else {
      message.tokens = "";
    }
    if (
      object.delegator_shares !== undefined &&
      object.delegator_shares !== null
    ) {
      message.delegator_shares = String(object.delegator_shares);
    } else {
      message.delegator_shares = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = Description.fromJSON(object.description);
    } else {
      message.description = undefined;
    }
    if (
      object.unbonding_height !== undefined &&
      object.unbonding_height !== null
    ) {
      message.unbonding_height = Number(object.unbonding_height);
    } else {
      message.unbonding_height = 0;
    }
    if (object.unbonding_time !== undefined && object.unbonding_time !== null) {
      message.unbonding_time = fromJsonTimestamp(object.unbonding_time);
    } else {
      message.unbonding_time = undefined;
    }
    if (object.commission !== undefined && object.commission !== null) {
      message.commission = Commission.fromJSON(object.commission);
    } else {
      message.commission = undefined;
    }
    if (
      object.min_self_delegation !== undefined &&
      object.min_self_delegation !== null
    ) {
      message.min_self_delegation = String(object.min_self_delegation);
    } else {
      message.min_self_delegation = "";
    }
    return message;
  },

  toJSON(message: Validator): unknown {
    const obj: any = {};
    message.operator_address !== undefined &&
      (obj.operator_address = message.operator_address);
    message.consensus_pubkey !== undefined &&
      (obj.consensus_pubkey = message.consensus_pubkey
        ? Any.toJSON(message.consensus_pubkey)
        : undefined);
    message.jailed !== undefined && (obj.jailed = message.jailed);
    message.status !== undefined &&
      (obj.status = bondStatusToJSON(message.status));
    message.tokens !== undefined && (obj.tokens = message.tokens);
    message.delegator_shares !== undefined &&
      (obj.delegator_shares = message.delegator_shares);
    message.description !== undefined &&
      (obj.description = message.description
        ? Description.toJSON(message.description)
        : undefined);
    message.unbonding_height !== undefined &&
      (obj.unbonding_height = message.unbonding_height);
    message.unbonding_time !== undefined &&
      (obj.unbonding_time =
        message.unbonding_time !== undefined
          ? message.unbonding_time.toISOString()
          : null);
    message.commission !== undefined &&
      (obj.commission = message.commission
        ? Commission.toJSON(message.commission)
        : undefined);
    message.min_self_delegation !== undefined &&
      (obj.min_self_delegation = message.min_self_delegation);
    return obj;
  },

  fromPartial(object: DeepPartial<Validator>): Validator {
    const message = { ...baseValidator } as Validator;
    if (
      object.operator_address !== undefined &&
      object.operator_address !== null
    ) {
      message.operator_address = object.operator_address;
    } else {
      message.operator_address = "";
    }
    if (
      object.consensus_pubkey !== undefined &&
      object.consensus_pubkey !== null
    ) {
      message.consensus_pubkey = Any.fromPartial(object.consensus_pubkey);
    } else {
      message.consensus_pubkey = undefined;
    }
    if (object.jailed !== undefined && object.jailed !== null) {
      message.jailed = object.jailed;
    } else {
      message.jailed = false;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = object.status;
    } else {
      message.status = 0;
    }
    if (object.tokens !== undefined && object.tokens !== null) {
      message.tokens = object.tokens;
    } else {
      message.tokens = "";
    }
    if (
      object.delegator_shares !== undefined &&
      object.delegator_shares !== null
    ) {
      message.delegator_shares = object.delegator_shares;
    } else {
      message.delegator_shares = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = Description.fromPartial(object.description);
    } else {
      message.description = undefined;
    }
    if (
      object.unbonding_height !== undefined &&
      object.unbonding_height !== null
    ) {
      message.unbonding_height = object.unbonding_height;
    } else {
      message.unbonding_height = 0;
    }
    if (object.unbonding_time !== undefined && object.unbonding_time !== null) {
      message.unbonding_time = object.unbonding_time;
    } else {
      message.unbonding_time = undefined;
    }
    if (object.commission !== undefined && object.commission !== null) {
      message.commission = Commission.fromPartial(object.commission);
    } else {
      message.commission = undefined;
    }
    if (
      object.min_self_delegation !== undefined &&
      object.min_self_delegation !== null
    ) {
      message.min_self_delegation = object.min_self_delegation;
    } else {
      message.min_self_delegation = "";
    }
    return message;
  },
};

const baseValAddresses: object = { addresses: "" };

export const ValAddresses = {
  encode(message: ValAddresses, writer: Writer = Writer.create()): Writer {
    for (const v of message.addresses) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValAddresses {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValAddresses } as ValAddresses;
    message.addresses = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.addresses.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValAddresses {
    const message = { ...baseValAddresses } as ValAddresses;
    message.addresses = [];
    if (object.addresses !== undefined && object.addresses !== null) {
      for (const e of object.addresses) {
        message.addresses.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: ValAddresses): unknown {
    const obj: any = {};
    if (message.addresses) {
      obj.addresses = message.addresses.map((e) => e);
    } else {
      obj.addresses = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ValAddresses>): ValAddresses {
    const message = { ...baseValAddresses } as ValAddresses;
    message.addresses = [];
    if (object.addresses !== undefined && object.addresses !== null) {
      for (const e of object.addresses) {
        message.addresses.push(e);
      }
    }
    return message;
  },
};

const baseDVPair: object = { delegator_address: "", validator_address: "" };

export const DVPair = {
  encode(message: DVPair, writer: Writer = Writer.create()): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.validator_address !== "") {
      writer.uint32(18).string(message.validator_address);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DVPair {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDVPair } as DVPair;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.validator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DVPair {
    const message = { ...baseDVPair } as DVPair;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    return message;
  },

  toJSON(message: DVPair): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    return obj;
  },

  fromPartial(object: DeepPartial<DVPair>): DVPair {
    const message = { ...baseDVPair } as DVPair;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    return message;
  },
};

const baseDVPairs: object = {};

export const DVPairs = {
  encode(message: DVPairs, writer: Writer = Writer.create()): Writer {
    for (const v of message.pairs) {
      DVPair.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DVPairs {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDVPairs } as DVPairs;
    message.pairs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pairs.push(DVPair.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DVPairs {
    const message = { ...baseDVPairs } as DVPairs;
    message.pairs = [];
    if (object.pairs !== undefined && object.pairs !== null) {
      for (const e of object.pairs) {
        message.pairs.push(DVPair.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: DVPairs): unknown {
    const obj: any = {};
    if (message.pairs) {
      obj.pairs = message.pairs.map((e) => (e ? DVPair.toJSON(e) : undefined));
    } else {
      obj.pairs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<DVPairs>): DVPairs {
    const message = { ...baseDVPairs } as DVPairs;
    message.pairs = [];
    if (object.pairs !== undefined && object.pairs !== null) {
      for (const e of object.pairs) {
        message.pairs.push(DVPair.fromPartial(e));
      }
    }
    return message;
  },
};

const baseDVVTriplet: object = {
  delegator_address: "",
  validator_src_address: "",
  validator_dst_address: "",
};

export const DVVTriplet = {
  encode(message: DVVTriplet, writer: Writer = Writer.create()): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.validator_src_address !== "") {
      writer.uint32(18).string(message.validator_src_address);
    }
    if (message.validator_dst_address !== "") {
      writer.uint32(26).string(message.validator_dst_address);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DVVTriplet {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDVVTriplet } as DVVTriplet;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.validator_src_address = reader.string();
          break;
        case 3:
          message.validator_dst_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DVVTriplet {
    const message = { ...baseDVVTriplet } as DVVTriplet;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_src_address !== undefined &&
      object.validator_src_address !== null
    ) {
      message.validator_src_address = String(object.validator_src_address);
    } else {
      message.validator_src_address = "";
    }
    if (
      object.validator_dst_address !== undefined &&
      object.validator_dst_address !== null
    ) {
      message.validator_dst_address = String(object.validator_dst_address);
    } else {
      message.validator_dst_address = "";
    }
    return message;
  },

  toJSON(message: DVVTriplet): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.validator_src_address !== undefined &&
      (obj.validator_src_address = message.validator_src_address);
    message.validator_dst_address !== undefined &&
      (obj.validator_dst_address = message.validator_dst_address);
    return obj;
  },

  fromPartial(object: DeepPartial<DVVTriplet>): DVVTriplet {
    const message = { ...baseDVVTriplet } as DVVTriplet;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_src_address !== undefined &&
      object.validator_src_address !== null
    ) {
      message.validator_src_address = object.validator_src_address;
    } else {
      message.validator_src_address = "";
    }
    if (
      object.validator_dst_address !== undefined &&
      object.validator_dst_address !== null
    ) {
      message.validator_dst_address = object.validator_dst_address;
    } else {
      message.validator_dst_address = "";
    }
    return message;
  },
};

const baseDVVTriplets: object = {};

export const DVVTriplets = {
  encode(message: DVVTriplets, writer: Writer = Writer.create()): Writer {
    for (const v of message.triplets) {
      DVVTriplet.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DVVTriplets {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDVVTriplets } as DVVTriplets;
    message.triplets = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.triplets.push(DVVTriplet.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DVVTriplets {
    const message = { ...baseDVVTriplets } as DVVTriplets;
    message.triplets = [];
    if (object.triplets !== undefined && object.triplets !== null) {
      for (const e of object.triplets) {
        message.triplets.push(DVVTriplet.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: DVVTriplets): unknown {
    const obj: any = {};
    if (message.triplets) {
      obj.triplets = message.triplets.map((e) =>
        e ? DVVTriplet.toJSON(e) : undefined
      );
    } else {
      obj.triplets = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<DVVTriplets>): DVVTriplets {
    const message = { ...baseDVVTriplets } as DVVTriplets;
    message.triplets = [];
    if (object.triplets !== undefined && object.triplets !== null) {
      for (const e of object.triplets) {
        message.triplets.push(DVVTriplet.fromPartial(e));
      }
    }
    return message;
  },
};

const baseDelegation: object = {
  delegator_address: "",
  validator_address: "",
  shares: "",
};

export const Delegation = {
  encode(message: Delegation, writer: Writer = Writer.create()): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.validator_address !== "") {
      writer.uint32(18).string(message.validator_address);
    }
    if (message.shares !== "") {
      writer.uint32(26).string(message.shares);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Delegation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDelegation } as Delegation;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.validator_address = reader.string();
          break;
        case 3:
          message.shares = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Delegation {
    const message = { ...baseDelegation } as Delegation;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    if (object.shares !== undefined && object.shares !== null) {
      message.shares = String(object.shares);
    } else {
      message.shares = "";
    }
    return message;
  },

  toJSON(message: Delegation): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    message.shares !== undefined && (obj.shares = message.shares);
    return obj;
  },

  fromPartial(object: DeepPartial<Delegation>): Delegation {
    const message = { ...baseDelegation } as Delegation;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    if (object.shares !== undefined && object.shares !== null) {
      message.shares = object.shares;
    } else {
      message.shares = "";
    }
    return message;
  },
};

const baseUnbondingDelegation: object = {
  delegator_address: "",
  validator_address: "",
};

export const UnbondingDelegation = {
  encode(
    message: UnbondingDelegation,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.validator_address !== "") {
      writer.uint32(18).string(message.validator_address);
    }
    for (const v of message.entries) {
      UnbondingDelegationEntry.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): UnbondingDelegation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseUnbondingDelegation } as UnbondingDelegation;
    message.entries = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.validator_address = reader.string();
          break;
        case 3:
          message.entries.push(
            UnbondingDelegationEntry.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): UnbondingDelegation {
    const message = { ...baseUnbondingDelegation } as UnbondingDelegation;
    message.entries = [];
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(UnbondingDelegationEntry.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: UnbondingDelegation): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    if (message.entries) {
      obj.entries = message.entries.map((e) =>
        e ? UnbondingDelegationEntry.toJSON(e) : undefined
      );
    } else {
      obj.entries = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<UnbondingDelegation>): UnbondingDelegation {
    const message = { ...baseUnbondingDelegation } as UnbondingDelegation;
    message.entries = [];
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(UnbondingDelegationEntry.fromPartial(e));
      }
    }
    return message;
  },
};

const baseUnbondingDelegationEntry: object = {
  creation_height: 0,
  initial_balance: "",
  balance: "",
};

export const UnbondingDelegationEntry = {
  encode(
    message: UnbondingDelegationEntry,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creation_height !== 0) {
      writer.uint32(8).int64(message.creation_height);
    }
    if (message.completion_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.completion_time),
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.initial_balance !== "") {
      writer.uint32(26).string(message.initial_balance);
    }
    if (message.balance !== "") {
      writer.uint32(34).string(message.balance);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): UnbondingDelegationEntry {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseUnbondingDelegationEntry,
    } as UnbondingDelegationEntry;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creation_height = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.completion_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.initial_balance = reader.string();
          break;
        case 4:
          message.balance = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): UnbondingDelegationEntry {
    const message = {
      ...baseUnbondingDelegationEntry,
    } as UnbondingDelegationEntry;
    if (
      object.creation_height !== undefined &&
      object.creation_height !== null
    ) {
      message.creation_height = Number(object.creation_height);
    } else {
      message.creation_height = 0;
    }
    if (
      object.completion_time !== undefined &&
      object.completion_time !== null
    ) {
      message.completion_time = fromJsonTimestamp(object.completion_time);
    } else {
      message.completion_time = undefined;
    }
    if (
      object.initial_balance !== undefined &&
      object.initial_balance !== null
    ) {
      message.initial_balance = String(object.initial_balance);
    } else {
      message.initial_balance = "";
    }
    if (object.balance !== undefined && object.balance !== null) {
      message.balance = String(object.balance);
    } else {
      message.balance = "";
    }
    return message;
  },

  toJSON(message: UnbondingDelegationEntry): unknown {
    const obj: any = {};
    message.creation_height !== undefined &&
      (obj.creation_height = message.creation_height);
    message.completion_time !== undefined &&
      (obj.completion_time =
        message.completion_time !== undefined
          ? message.completion_time.toISOString()
          : null);
    message.initial_balance !== undefined &&
      (obj.initial_balance = message.initial_balance);
    message.balance !== undefined && (obj.balance = message.balance);
    return obj;
  },

  fromPartial(
    object: DeepPartial<UnbondingDelegationEntry>
  ): UnbondingDelegationEntry {
    const message = {
      ...baseUnbondingDelegationEntry,
    } as UnbondingDelegationEntry;
    if (
      object.creation_height !== undefined &&
      object.creation_height !== null
    ) {
      message.creation_height = object.creation_height;
    } else {
      message.creation_height = 0;
    }
    if (
      object.completion_time !== undefined &&
      object.completion_time !== null
    ) {
      message.completion_time = object.completion_time;
    } else {
      message.completion_time = undefined;
    }
    if (
      object.initial_balance !== undefined &&
      object.initial_balance !== null
    ) {
      message.initial_balance = object.initial_balance;
    } else {
      message.initial_balance = "";
    }
    if (object.balance !== undefined && object.balance !== null) {
      message.balance = object.balance;
    } else {
      message.balance = "";
    }
    return message;
  },
};

const baseRedelegationEntry: object = {
  creation_height: 0,
  initial_balance: "",
  shares_dst: "",
};

export const RedelegationEntry = {
  encode(message: RedelegationEntry, writer: Writer = Writer.create()): Writer {
    if (message.creation_height !== 0) {
      writer.uint32(8).int64(message.creation_height);
    }
    if (message.completion_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.completion_time),
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.initial_balance !== "") {
      writer.uint32(26).string(message.initial_balance);
    }
    if (message.shares_dst !== "") {
      writer.uint32(34).string(message.shares_dst);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): RedelegationEntry {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseRedelegationEntry } as RedelegationEntry;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creation_height = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.completion_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.initial_balance = reader.string();
          break;
        case 4:
          message.shares_dst = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): RedelegationEntry {
    const message = { ...baseRedelegationEntry } as RedelegationEntry;
    if (
      object.creation_height !== undefined &&
      object.creation_height !== null
    ) {
      message.creation_height = Number(object.creation_height);
    } else {
      message.creation_height = 0;
    }
    if (
      object.completion_time !== undefined &&
      object.completion_time !== null
    ) {
      message.completion_time = fromJsonTimestamp(object.completion_time);
    } else {
      message.completion_time = undefined;
    }
    if (
      object.initial_balance !== undefined &&
      object.initial_balance !== null
    ) {
      message.initial_balance = String(object.initial_balance);
    } else {
      message.initial_balance = "";
    }
    if (object.shares_dst !== undefined && object.shares_dst !== null) {
      message.shares_dst = String(object.shares_dst);
    } else {
      message.shares_dst = "";
    }
    return message;
  },

  toJSON(message: RedelegationEntry): unknown {
    const obj: any = {};
    message.creation_height !== undefined &&
      (obj.creation_height = message.creation_height);
    message.completion_time !== undefined &&
      (obj.completion_time =
        message.completion_time !== undefined
          ? message.completion_time.toISOString()
          : null);
    message.initial_balance !== undefined &&
      (obj.initial_balance = message.initial_balance);
    message.shares_dst !== undefined && (obj.shares_dst = message.shares_dst);
    return obj;
  },

  fromPartial(object: DeepPartial<RedelegationEntry>): RedelegationEntry {
    const message = { ...baseRedelegationEntry } as RedelegationEntry;
    if (
      object.creation_height !== undefined &&
      object.creation_height !== null
    ) {
      message.creation_height = object.creation_height;
    } else {
      message.creation_height = 0;
    }
    if (
      object.completion_time !== undefined &&
      object.completion_time !== null
    ) {
      message.completion_time = object.completion_time;
    } else {
      message.completion_time = undefined;
    }
    if (
      object.initial_balance !== undefined &&
      object.initial_balance !== null
    ) {
      message.initial_balance = object.initial_balance;
    } else {
      message.initial_balance = "";
    }
    if (object.shares_dst !== undefined && object.shares_dst !== null) {
      message.shares_dst = object.shares_dst;
    } else {
      message.shares_dst = "";
    }
    return message;
  },
};

const baseRedelegation: object = {
  delegator_address: "",
  validator_src_address: "",
  validator_dst_address: "",
};

export const Redelegation = {
  encode(message: Redelegation, writer: Writer = Writer.create()): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.validator_src_address !== "") {
      writer.uint32(18).string(message.validator_src_address);
    }
    if (message.validator_dst_address !== "") {
      writer.uint32(26).string(message.validator_dst_address);
    }
    for (const v of message.entries) {
      RedelegationEntry.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Redelegation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseRedelegation } as Redelegation;
    message.entries = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.validator_src_address = reader.string();
          break;
        case 3:
          message.validator_dst_address = reader.string();
          break;
        case 4:
          message.entries.push(
            RedelegationEntry.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Redelegation {
    const message = { ...baseRedelegation } as Redelegation;
    message.entries = [];
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_src_address !== undefined &&
      object.validator_src_address !== null
    ) {
      message.validator_src_address = String(object.validator_src_address);
    } else {
      message.validator_src_address = "";
    }
    if (
      object.validator_dst_address !== undefined &&
      object.validator_dst_address !== null
    ) {
      message.validator_dst_address = String(object.validator_dst_address);
    } else {
      message.validator_dst_address = "";
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(RedelegationEntry.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: Redelegation): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.validator_src_address !== undefined &&
      (obj.validator_src_address = message.validator_src_address);
    message.validator_dst_address !== undefined &&
      (obj.validator_dst_address = message.validator_dst_address);
    if (message.entries) {
      obj.entries = message.entries.map((e) =>
        e ? RedelegationEntry.toJSON(e) : undefined
      );
    } else {
      obj.entries = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Redelegation>): Redelegation {
    const message = { ...baseRedelegation } as Redelegation;
    message.entries = [];
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_src_address !== undefined &&
      object.validator_src_address !== null
    ) {
      message.validator_src_address = object.validator_src_address;
    } else {
      message.validator_src_address = "";
    }
    if (
      object.validator_dst_address !== undefined &&
      object.validator_dst_address !== null
    ) {
      message.validator_dst_address = object.validator_dst_address;
    } else {
      message.validator_dst_address = "";
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(RedelegationEntry.fromPartial(e));
      }
    }
    return message;
  },
};

const baseParams: object = {
  max_validators: 0,
  max_entries: 0,
  historical_entries: 0,
  bond_denom: "",
};

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.unbonding_time !== undefined) {
      Duration.encode(
        message.unbonding_time,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.max_validators !== 0) {
      writer.uint32(16).uint32(message.max_validators);
    }
    if (message.max_entries !== 0) {
      writer.uint32(24).uint32(message.max_entries);
    }
    if (message.historical_entries !== 0) {
      writer.uint32(32).uint32(message.historical_entries);
    }
    if (message.bond_denom !== "") {
      writer.uint32(42).string(message.bond_denom);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.unbonding_time = Duration.decode(reader, reader.uint32());
          break;
        case 2:
          message.max_validators = reader.uint32();
          break;
        case 3:
          message.max_entries = reader.uint32();
          break;
        case 4:
          message.historical_entries = reader.uint32();
          break;
        case 5:
          message.bond_denom = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Params {
    const message = { ...baseParams } as Params;
    if (object.unbonding_time !== undefined && object.unbonding_time !== null) {
      message.unbonding_time = Duration.fromJSON(object.unbonding_time);
    } else {
      message.unbonding_time = undefined;
    }
    if (object.max_validators !== undefined && object.max_validators !== null) {
      message.max_validators = Number(object.max_validators);
    } else {
      message.max_validators = 0;
    }
    if (object.max_entries !== undefined && object.max_entries !== null) {
      message.max_entries = Number(object.max_entries);
    } else {
      message.max_entries = 0;
    }
    if (
      object.historical_entries !== undefined &&
      object.historical_entries !== null
    ) {
      message.historical_entries = Number(object.historical_entries);
    } else {
      message.historical_entries = 0;
    }
    if (object.bond_denom !== undefined && object.bond_denom !== null) {
      message.bond_denom = String(object.bond_denom);
    } else {
      message.bond_denom = "";
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.unbonding_time !== undefined &&
      (obj.unbonding_time = message.unbonding_time
        ? Duration.toJSON(message.unbonding_time)
        : undefined);
    message.max_validators !== undefined &&
      (obj.max_validators = message.max_validators);
    message.max_entries !== undefined &&
      (obj.max_entries = message.max_entries);
    message.historical_entries !== undefined &&
      (obj.historical_entries = message.historical_entries);
    message.bond_denom !== undefined && (obj.bond_denom = message.bond_denom);
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (object.unbonding_time !== undefined && object.unbonding_time !== null) {
      message.unbonding_time = Duration.fromPartial(object.unbonding_time);
    } else {
      message.unbonding_time = undefined;
    }
    if (object.max_validators !== undefined && object.max_validators !== null) {
      message.max_validators = object.max_validators;
    } else {
      message.max_validators = 0;
    }
    if (object.max_entries !== undefined && object.max_entries !== null) {
      message.max_entries = object.max_entries;
    } else {
      message.max_entries = 0;
    }
    if (
      object.historical_entries !== undefined &&
      object.historical_entries !== null
    ) {
      message.historical_entries = object.historical_entries;
    } else {
      message.historical_entries = 0;
    }
    if (object.bond_denom !== undefined && object.bond_denom !== null) {
      message.bond_denom = object.bond_denom;
    } else {
      message.bond_denom = "";
    }
    return message;
  },
};

const baseDelegationResponse: object = {};

export const DelegationResponse = {
  encode(
    message: DelegationResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegation !== undefined) {
      Delegation.encode(message.delegation, writer.uint32(10).fork()).ldelim();
    }
    if (message.balance !== undefined) {
      Coin.encode(message.balance, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DelegationResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDelegationResponse } as DelegationResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegation = Delegation.decode(reader, reader.uint32());
          break;
        case 2:
          message.balance = Coin.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DelegationResponse {
    const message = { ...baseDelegationResponse } as DelegationResponse;
    if (object.delegation !== undefined && object.delegation !== null) {
      message.delegation = Delegation.fromJSON(object.delegation);
    } else {
      message.delegation = undefined;
    }
    if (object.balance !== undefined && object.balance !== null) {
      message.balance = Coin.fromJSON(object.balance);
    } else {
      message.balance = undefined;
    }
    return message;
  },

  toJSON(message: DelegationResponse): unknown {
    const obj: any = {};
    message.delegation !== undefined &&
      (obj.delegation = message.delegation
        ? Delegation.toJSON(message.delegation)
        : undefined);
    message.balance !== undefined &&
      (obj.balance = message.balance
        ? Coin.toJSON(message.balance)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<DelegationResponse>): DelegationResponse {
    const message = { ...baseDelegationResponse } as DelegationResponse;
    if (object.delegation !== undefined && object.delegation !== null) {
      message.delegation = Delegation.fromPartial(object.delegation);
    } else {
      message.delegation = undefined;
    }
    if (object.balance !== undefined && object.balance !== null) {
      message.balance = Coin.fromPartial(object.balance);
    } else {
      message.balance = undefined;
    }
    return message;
  },
};

const baseRedelegationEntryResponse: object = { balance: "" };

export const RedelegationEntryResponse = {
  encode(
    message: RedelegationEntryResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.redelegation_entry !== undefined) {
      RedelegationEntry.encode(
        message.redelegation_entry,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.balance !== "") {
      writer.uint32(34).string(message.balance);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): RedelegationEntryResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseRedelegationEntryResponse,
    } as RedelegationEntryResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.redelegation_entry = RedelegationEntry.decode(
            reader,
            reader.uint32()
          );
          break;
        case 4:
          message.balance = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): RedelegationEntryResponse {
    const message = {
      ...baseRedelegationEntryResponse,
    } as RedelegationEntryResponse;
    if (
      object.redelegation_entry !== undefined &&
      object.redelegation_entry !== null
    ) {
      message.redelegation_entry = RedelegationEntry.fromJSON(
        object.redelegation_entry
      );
    } else {
      message.redelegation_entry = undefined;
    }
    if (object.balance !== undefined && object.balance !== null) {
      message.balance = String(object.balance);
    } else {
      message.balance = "";
    }
    return message;
  },

  toJSON(message: RedelegationEntryResponse): unknown {
    const obj: any = {};
    message.redelegation_entry !== undefined &&
      (obj.redelegation_entry = message.redelegation_entry
        ? RedelegationEntry.toJSON(message.redelegation_entry)
        : undefined);
    message.balance !== undefined && (obj.balance = message.balance);
    return obj;
  },

  fromPartial(
    object: DeepPartial<RedelegationEntryResponse>
  ): RedelegationEntryResponse {
    const message = {
      ...baseRedelegationEntryResponse,
    } as RedelegationEntryResponse;
    if (
      object.redelegation_entry !== undefined &&
      object.redelegation_entry !== null
    ) {
      message.redelegation_entry = RedelegationEntry.fromPartial(
        object.redelegation_entry
      );
    } else {
      message.redelegation_entry = undefined;
    }
    if (object.balance !== undefined && object.balance !== null) {
      message.balance = object.balance;
    } else {
      message.balance = "";
    }
    return message;
  },
};

const baseRedelegationResponse: object = {};

export const RedelegationResponse = {
  encode(
    message: RedelegationResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.redelegation !== undefined) {
      Redelegation.encode(
        message.redelegation,
        writer.uint32(10).fork()
      ).ldelim();
    }
    for (const v of message.entries) {
      RedelegationEntryResponse.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): RedelegationResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseRedelegationResponse } as RedelegationResponse;
    message.entries = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.redelegation = Redelegation.decode(reader, reader.uint32());
          break;
        case 2:
          message.entries.push(
            RedelegationEntryResponse.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): RedelegationResponse {
    const message = { ...baseRedelegationResponse } as RedelegationResponse;
    message.entries = [];
    if (object.redelegation !== undefined && object.redelegation !== null) {
      message.redelegation = Redelegation.fromJSON(object.redelegation);
    } else {
      message.redelegation = undefined;
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(RedelegationEntryResponse.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: RedelegationResponse): unknown {
    const obj: any = {};
    message.redelegation !== undefined &&
      (obj.redelegation = message.redelegation
        ? Redelegation.toJSON(message.redelegation)
        : undefined);
    if (message.entries) {
      obj.entries = message.entries.map((e) =>
        e ? RedelegationEntryResponse.toJSON(e) : undefined
      );
    } else {
      obj.entries = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<RedelegationResponse>): RedelegationResponse {
    const message = { ...baseRedelegationResponse } as RedelegationResponse;
    message.entries = [];
    if (object.redelegation !== undefined && object.redelegation !== null) {
      message.redelegation = Redelegation.fromPartial(object.redelegation);
    } else {
      message.redelegation = undefined;
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(RedelegationEntryResponse.fromPartial(e));
      }
    }
    return message;
  },
};

const basePool: object = { not_bonded_tokens: "", bonded_tokens: "" };

export const Pool = {
  encode(message: Pool, writer: Writer = Writer.create()): Writer {
    if (message.not_bonded_tokens !== "") {
      writer.uint32(10).string(message.not_bonded_tokens);
    }
    if (message.bonded_tokens !== "") {
      writer.uint32(18).string(message.bonded_tokens);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Pool {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePool } as Pool;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.not_bonded_tokens = reader.string();
          break;
        case 2:
          message.bonded_tokens = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Pool {
    const message = { ...basePool } as Pool;
    if (
      object.not_bonded_tokens !== undefined &&
      object.not_bonded_tokens !== null
    ) {
      message.not_bonded_tokens = String(object.not_bonded_tokens);
    } else {
      message.not_bonded_tokens = "";
    }
    if (object.bonded_tokens !== undefined && object.bonded_tokens !== null) {
      message.bonded_tokens = String(object.bonded_tokens);
    } else {
      message.bonded_tokens = "";
    }
    return message;
  },

  toJSON(message: Pool): unknown {
    const obj: any = {};
    message.not_bonded_tokens !== undefined &&
      (obj.not_bonded_tokens = message.not_bonded_tokens);
    message.bonded_tokens !== undefined &&
      (obj.bonded_tokens = message.bonded_tokens);
    return obj;
  },

  fromPartial(object: DeepPartial<Pool>): Pool {
    const message = { ...basePool } as Pool;
    if (
      object.not_bonded_tokens !== undefined &&
      object.not_bonded_tokens !== null
    ) {
      message.not_bonded_tokens = object.not_bonded_tokens;
    } else {
      message.not_bonded_tokens = "";
    }
    if (object.bonded_tokens !== undefined && object.bonded_tokens !== null) {
      message.bonded_tokens = object.bonded_tokens;
    } else {
      message.bonded_tokens = "";
    }
    return message;
  },
};

declare var self: any | undefined;
declare var window: any | undefined;
var globalThis: any = (() => {
  if (typeof globalThis !== "undefined") return globalThis;
  if (typeof self !== "undefined") return self;
  if (typeof window !== "undefined") return window;
  if (typeof global !== "undefined") return global;
  throw "Unable to locate global object";
})();

type Builtin = Date | Function | Uint8Array | string | number | undefined;
export type DeepPartial<T> = T extends Builtin
  ? T
  : T extends Array<infer U>
  ? Array<DeepPartial<U>>
  : T extends ReadonlyArray<infer U>
  ? ReadonlyArray<DeepPartial<U>>
  : T extends {}
  ? { [K in keyof T]?: DeepPartial<T[K]> }
  : Partial<T>;

function toTimestamp(date: Date): Timestamp {
  const seconds = date.getTime() / 1_000;
  const nanos = (date.getTime() % 1_000) * 1_000_000;
  return { seconds, nanos };
}

function fromTimestamp(t: Timestamp): Date {
  let millis = t.seconds * 1_000;
  millis += t.nanos / 1_000_000;
  return new Date(millis);
}

function fromJsonTimestamp(o: any): Date {
  if (o instanceof Date) {
    return o;
  } else if (typeof o === "string") {
    return new Date(o);
  } else {
    return fromTimestamp(Timestamp.fromJSON(o));
  }
}

function longToNumber(long: Long): number {
  if (long.gt(Number.MAX_SAFE_INTEGER)) {
    throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
  }
  return long.toNumber();
}

if (util.Long !== Long) {
  util.Long = Long as any;
  configure();
}
