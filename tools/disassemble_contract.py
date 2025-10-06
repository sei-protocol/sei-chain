#!/usr/bin/env python3

"""Utility to disassemble EVM bytecode without external dependencies."""

from __future__ import annotations

import argparse
import sys
from textwrap import dedent

# Mapping of opcodes to mnemonics. Only the opcodes used by the provided bytecode
# are enumerated to keep the table compact.
OPCODES = {
    0x00: "STOP",
    0x01: "ADD",
    0x02: "MUL",
    0x03: "SUB",
    0x04: "DIV",
    0x05: "SDIV",
    0x06: "MOD",
    0x07: "SMOD",
    0x08: "ADDMOD",
    0x09: "MULMOD",
    0x0A: "EXP",
    0x0B: "SIGNEXTEND",
    0x10: "LT",
    0x11: "GT",
    0x12: "SLT",
    0x13: "SGT",
    0x14: "EQ",
    0x15: "ISZERO",
    0x16: "AND",
    0x17: "OR",
    0x18: "XOR",
    0x19: "NOT",
    0x1A: "BYTE",
    0x1B: "SHL",
    0x1C: "SHR",
    0x1D: "SAR",
    0x20: "SHA3",
    0x30: "ADDRESS",
    0x31: "BALANCE",
    0x32: "ORIGIN",
    0x33: "CALLER",
    0x34: "CALLVALUE",
    0x35: "CALLDATALOAD",
    0x36: "CALLDATASIZE",
    0x37: "CALLDATACOPY",
    0x38: "CODESIZE",
    0x39: "CODECOPY",
    0x3A: "GASPRICE",
    0x3B: "EXTCODESIZE",
    0x3C: "EXTCODECOPY",
    0x3D: "RETURNDATASIZE",
    0x3E: "RETURNDATACOPY",
    0x3F: "EXTCODEHASH",
    0x40: "BLOCKHASH",
    0x41: "COINBASE",
    0x42: "TIMESTAMP",
    0x43: "NUMBER",
    0x44: "DIFFICULTY",
    0x45: "GASLIMIT",
    0x46: "CHAINID",
    0x47: "SELFBALANCE",
    0x48: "BASEFEE",
    0x49: "BLOBHASH",
    0x4A: "BLOBBASEFEE",
    0x50: "POP",
    0x51: "MLOAD",
    0x52: "MSTORE",
    0x53: "MSTORE8",
    0x54: "SLOAD",
    0x55: "SSTORE",
    0x56: "JUMP",
    0x57: "JUMPI",
    0x58: "PC",
    0x59: "MSIZE",
    0x5A: "GAS",
    0x5B: "JUMPDEST",
    0xF0: "CREATE",
    0xF1: "CALL",
    0xF2: "CALLCODE",
    0xF3: "RETURN",
    0xF4: "DELEGATECALL",
    0xF5: "CREATE2",
    0xFA: "REVERT",
    0xFD: "REVERT",
    0xFE: "INVALID",
}

for i in range(1, 33):
    OPCODES[0x5F + i] = f"PUSH{i}"
for i in range(1, 17):
    OPCODES[0x7F + i] = f"DUP{i}"
for i in range(1, 17):
    OPCODES[0x8F + i] = f"SWAP{i}"
for i in range(0xA0, 0xA4):
    OPCODES[i] = f"LOG{i - 0xA0}"


def _strip_0x(value: str) -> str:
    return value[2:] if value.startswith("0x") else value


def disassemble(bytecode: str) -> list[str]:
    """Return a textual disassembly of *bytecode*."""

    cleaned = _strip_0x(bytecode)
    if len(cleaned) % 2 != 0:
        raise ValueError("Bytecode length must be even")

    output: list[str] = []
    pc = 0
    code = bytes.fromhex(cleaned)
    length = len(code)

    while pc < length:
        opcode = code[pc]
        mnemonic = OPCODES.get(opcode, f"OP_{opcode:02x}")
        if 0x60 <= opcode <= 0x7F:
            size = opcode - 0x5F
            if pc + 1 + size > length:
                raise ValueError("PUSH extends past end of bytecode")
            data = code[pc + 1 : pc + 1 + size]
            output.append(f"{pc:04x}: {mnemonic} 0x{data.hex()}")
            pc += 1 + size
        else:
            output.append(f"{pc:04x}: {mnemonic}")
            pc += 1

    return output


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Disassemble raw EVM bytecode",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=dedent(
            """
            Examples:
              %(prog)s --bytecode 0x60806040...
              %(prog)s --bytecode-file path/to/bytecode.txt
            """
        ),
    )
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--bytecode", help="Hex encoded bytecode")
    group.add_argument("--bytecode-file", help="Read bytecode from file")
    args = parser.parse_args(argv)

    if args.bytecode:
        bytecode = args.bytecode.strip()
    else:
        with open(args.bytecode_file, "r", encoding="utf-8") as handle:
            bytecode = handle.read().strip()

    try:
        disassembled = disassemble(bytecode)
    except ValueError as exc:  # pragma: no cover - CLI error handling
        print(f"error: {exc}", file=sys.stderr)
        return 1

    for line in disassembled:
        print(line)
    return 0


if __name__ == "__main__":  # pragma: no cover - CLI entry point
    raise SystemExit(main(sys.argv[1:]))
