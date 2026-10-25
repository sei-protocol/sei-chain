package query

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WrapGRPCError returns err unchanged when it already carries a gRPC status code,
// otherwise wraps it as Internal.
func WrapGRPCError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Error(codes.Internal, err.Error())
}
