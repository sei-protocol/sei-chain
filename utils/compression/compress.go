package compression

import (
	"fmt"
	
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func CompressMessage(message proto.Message) ([]byte, error) {
	b, err := proto.Marshal(message)
	if err != nil {
		return nil, err
	}

	bCompressed, err := compressZLib(b)
	if err != nil {
		return nil, err
	}

	cd := &types.CompressedData{
		Data:      bCompressed,
		Algorithm: types.CompressedData_ZLIB,
	}
	return cd.Marshal()
}

func DecompressMessage(target proto.Message, compressed []byte) error {
	// this will work if the type is CompressedData
	// if not, then it will try to unmarshal it as a regular proto message
	var cd types.CompressedData
	if err := cd.Unmarshal(compressed); err != nil {
		// fall back to non-compress unmarshal
		return proto.Unmarshal(compressed, target)
	}

	// unmarshal was successful, but no data, treat as non-compressed
	if cd.Data == nil {
		return proto.Unmarshal(compressed, target)
	}

	// add other algorithms here if we need to change it
	if cd.Algorithm == types.CompressedData_ZLIB {
		decompressed, err := decompressZLib(cd.Data)
		if err != nil {
			return err
		}
		return proto.Unmarshal(decompressed, target)
	}

	return fmt.Errorf("unsupported compression algorithm: %d (%s)", cd.Algorithm, cd.Algorithm.String())
}
