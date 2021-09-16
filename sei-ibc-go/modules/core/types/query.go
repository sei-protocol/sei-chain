package types

import (
	"github.com/gogo/protobuf/grpc"

	client "github.com/cosmos/ibc-go/v2/modules/core/02-client"
	clienttypes "github.com/cosmos/ibc-go/v2/modules/core/02-client/types"
	connection "github.com/cosmos/ibc-go/v2/modules/core/03-connection"
	connectiontypes "github.com/cosmos/ibc-go/v2/modules/core/03-connection/types"
	channel "github.com/cosmos/ibc-go/v2/modules/core/04-channel"
	channeltypes "github.com/cosmos/ibc-go/v2/modules/core/04-channel/types"
	port "github.com/cosmos/ibc-go/v2/modules/core/05-port"
	porttypes "github.com/cosmos/ibc-go/v2/modules/core/05-port/types"
)

// QueryServer defines the IBC interfaces that the gRPC query server must implement
type QueryServer interface {
	clienttypes.QueryServer
	connectiontypes.QueryServer
	channeltypes.QueryServer
	porttypes.QueryServer
}

// RegisterQueryService registers each individual IBC submodule query service
func RegisterQueryService(server grpc.Server, queryService QueryServer) {
	client.RegisterQueryService(server, queryService)
	connection.RegisterQueryService(server, queryService)
	channel.RegisterQueryService(server, queryService)
	port.RegisterQueryService(server, queryService)
}
