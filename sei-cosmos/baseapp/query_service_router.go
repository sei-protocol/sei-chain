package baseapp

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/grpc/reflection"

	gogogrpc "github.com/gogo/protobuf/grpc"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/protoadapt"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// QueryServiceRouter routes ABCI Query requests to protobuf Query service handlers
type QueryServiceRouter struct {
	routes            map[string]QueryServiceHandler
	interfaceRegistry codectypes.InterfaceRegistry
	serviceData       []serviceData
}

// serviceData represents a gRPC service, along with its handler.
type serviceData struct {
	serviceDesc *grpc.ServiceDesc
	handler     interface{}
}

var _ gogogrpc.Server = &QueryServiceRouter{}

// NewQueryServiceRouter creates a new QueryServiceRouter
func NewQueryServiceRouter() *QueryServiceRouter {
	return &QueryServiceRouter{
		routes: map[string]QueryServiceHandler{},
	}
}

// QueryServiceHandler defines a function type which handles ABCI Query requests
// with a protobuf Query service
type QueryServiceHandler = func(ctx sdk.Context, req abci.RequestQuery) (abci.ResponseQuery, error)

// Route returns the QueryServiceHandler for a given query route path or nil
// if not found
func (qrt *QueryServiceRouter) Route(path string) QueryServiceHandler {
	handler, found := qrt.routes[path]
	if !found {
		return nil
	}
	return handler
}

// RegisterService implements the gRPC Server.RegisterService method. sd is a gRPC
// service description, handler is an object which implements that gRPC service/
//
// This functions PANICS:
// - if a protobuf service is registered twice.
func (qrt *QueryServiceRouter) RegisterService(sd *grpc.ServiceDesc, handler interface{}) {
	// adds a top-level query handler based on the gRPC service name
	for _, method := range sd.Methods {
		fqName := fmt.Sprintf("/%s/%s", sd.ServiceName, method.MethodName)
		methodHandler := method.Handler

		// Check that each service is only registered once. If a service is
		// registered more than once, then we should error. Since we can't
		// return an error (`Server.RegisterService` interface restriction) we
		// panic (at startup).
		_, found := qrt.routes[fqName]
		if found {
			panic(
				fmt.Errorf(
					"gRPC query service %s has already been registered. Please make sure to only register each service once. "+
						"This usually means that there are conflicting modules registering the same gRPC query service",
					fqName,
				),
			)
		}

		qrt.routes[fqName] = func(ctx sdk.Context, req abci.RequestQuery) (abci.ResponseQuery, error) {
			// call the method handler from the service description with the handler object,
			// a wrapped sdk.Context with proto-unmarshaled data from the ABCI request data
			res, err := methodHandler(handler, sdk.WrapSDKContext(ctx), func(i interface{}) error {
				err := unmarshalProto(req.Data, i)
				if err != nil {
					return err
				}
				if qrt.interfaceRegistry != nil {
					return codectypes.UnpackInterfaces(i, qrt.interfaceRegistry)
				}
				return nil
			}, nil)
			if err != nil {
				return abci.ResponseQuery{}, err
			}

			// proto marshal the result bytes
			resBytes, err := marshalProto(res)
			if err != nil {
				return abci.ResponseQuery{}, err
			}

			// return the result bytes as the response value
			return abci.ResponseQuery{
				Height: req.Height,
				Value:  resBytes,
			}, nil
		}
	}

	qrt.serviceData = append(qrt.serviceData, serviceData{
		serviceDesc: sd,
		handler:     handler,
	})
}

// SetInterfaceRegistry sets the interface registry for the router. This will
// also register the interface reflection gRPC service.
func (qrt *QueryServiceRouter) SetInterfaceRegistry(interfaceRegistry codectypes.InterfaceRegistry) {
	qrt.interfaceRegistry = interfaceRegistry
	// Once we have an interface registry, we can register the interface
	// registry reflection gRPC service.
	reflection.RegisterReflectionServiceServer(qrt, reflection.NewReflectionServiceServer(interfaceRegistry))
}

// QueryServiceRouter returns the QueryServiceRouter of a BaseApp.
func (app *BaseApp) QueryServiceRouter() *QueryServiceRouter { return app.queryServiceRouter }

// marshalProto encodes a protobuf message. It returns an error if v is not a protobuf message.
func marshalProto(v any) ([]byte, error) {
	message, ok := v.(protoadapt.MessageV1)
	if !ok {
		return nil, fmt.Errorf("failed to marshal, message is %T, want proto.Message", v)
	}
	data, err := proto.Marshal(protoadapt.MessageV2Of(message))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %T: %w", v, err)
	}
	return data, nil
}

// unmarshalProto decodes data into a protobuf message. It returns an error if v is not a protobuf message.
func unmarshalProto(data []byte, v any) error {
	message, ok := v.(protoadapt.MessageV1)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	if err := proto.Unmarshal(data, protoadapt.MessageV2Of(message)); err != nil {
		return fmt.Errorf("failed to unmarshal %T: %w", v, err)
	}
	return nil
}
