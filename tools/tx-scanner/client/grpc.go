package client

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

var GrpcConn *grpc.ClientConn

func InitializeGRPCClient(targetEndpoint string, port int) {
	var dialOptions []grpc.DialOption

	// Use default insecure if we don't have credentials setup
	dialOptions = append(dialOptions, grpc.WithInsecure())
	dialOptions = append(dialOptions, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(20*1024*1024),
		grpc.MaxCallSendMsgSize(20*1024*1024)),
	)
	dialOptions = append(dialOptions, grpc.WithBlock())
	if GrpcConn == nil {
		grpcConn, err := grpc.Dial(
			fmt.Sprintf("%s:%d", targetEndpoint, port),
			dialOptions...,
		)
		if err != nil {
			fmt.Printf("Failed to connect to %s:%d: %s\n", targetEndpoint, port, err.Error())
		}
		GrpcConn = grpcConn
	}
	// spin up goroutine for monitoring and reconnect purposes
	go func() {
		for {
			state := GrpcConn.GetState()
			if state == connectivity.TransientFailure || state == connectivity.Shutdown {
				fmt.Println("GRPC Connection lost, attempting to reconnect...")
				for {
					if GrpcConn.WaitForStateChange(context.Background(), state) {
						break
					}
					time.Sleep(10 * time.Second)
				}
			}
			time.Sleep(10 * time.Second)
		}
	}()
}

func GetTmServiceClient() tmservice.ServiceClient {
	return tmservice.NewServiceClient(GrpcConn)
}

func GetTxClient() txtypes.ServiceClient {
	return txtypes.NewServiceClient(GrpcConn)
}
