package flatkv

import (
	"unsafe"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// accountCodec converts account rows to and from the account database's bytes.
var accountCodec = view.Codec[vtype.AccountData]{
	Append: vtype.AppendAccountData,
	Decode: vtype.DeserializeAccountData,
	Size:   func(vtype.AccountData) uint64 { return uint64(unsafe.Sizeof(vtype.AccountData{})) },
}

// codeCodec converts code rows to and from the code database's bytes.
var codeCodec = view.Codec[vtype.CodeData]{
	Append: vtype.AppendCodeData,
	Decode: vtype.DeserializeCodeData,
	Size: func(code vtype.CodeData) uint64 {
		return uint64(unsafe.Sizeof(code)) + uint64(len(code.GetBytecode()))
	},
}

// storageCodec converts storage rows to and from the storage database's bytes.
var storageCodec = view.Codec[vtype.StorageData]{
	Append: vtype.AppendStorageData,
	Decode: vtype.DeserializeStorageData,
	Size:   func(vtype.StorageData) uint64 { return uint64(unsafe.Sizeof(vtype.StorageData{})) },
}

// miscCodec converts misc rows to and from the misc database's bytes.
var miscCodec = view.Codec[vtype.MiscData]{
	Append: vtype.AppendMiscData,
	Decode: vtype.DeserializeMiscData,
	Size: func(misc vtype.MiscData) uint64 {
		return uint64(unsafe.Sizeof(misc)) + uint64(len(misc.GetValue()))
	},
}
