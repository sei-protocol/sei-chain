package types

// NewMetadata creates and returns a new ICS27 Metadata instance
func NewMetadata(version, controllerConnectionID, hostConnectionID, accAddress string) Metadata {
	return Metadata{
		Version:                version,
		ControllerConnectionId: controllerConnectionID,
		HostConnectionId:       hostConnectionID,
		Address:                accAddress,
	}
}
