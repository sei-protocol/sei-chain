/* eslint-disable */
export const protobufPackage = "sei.evm.v1";

export enum PointerType {
  ERC20 = 0,
  ERC721 = 1,
  NATIVE = 2,
  CW20 = 3,
  CW721 = 4,
  UNRECOGNIZED = -1,
}

export function pointerTypeFromJSON(object: any): PointerType {
  switch (object) {
    case 0:
    case "ERC20":
      return PointerType.ERC20;
    case 1:
    case "ERC721":
      return PointerType.ERC721;
    case 2:
    case "NATIVE":
      return PointerType.NATIVE;
    case 3:
    case "CW20":
      return PointerType.CW20;
    case 4:
    case "CW721":
      return PointerType.CW721;
    case -1:
    case "UNRECOGNIZED":
    default:
      return PointerType.UNRECOGNIZED;
  }
}

export function pointerTypeToJSON(object: PointerType): string {
  switch (object) {
    case PointerType.ERC20:
      return "ERC20";
    case PointerType.ERC721:
      return "ERC721";
    case PointerType.NATIVE:
      return "NATIVE";
    case PointerType.CW20:
      return "CW20";
    case PointerType.CW721:
      return "CW721";
    default:
      return "UNKNOWN";
  }
}
