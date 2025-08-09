package types

// NewQueryAppVersionResponse creates a new QueryAppVersionResponse instance
func NewQueryAppVersionResponse(portID, version string) *QueryAppVersionResponse {
	return &QueryAppVersionResponse{
		PortId:  portID,
		Version: version,
	}
}
