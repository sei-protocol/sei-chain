package accesscontrol

import (
	"fmt"
)

// Alias for Map of MessageIndex -> AccessOperation -> Channel
type MessageAccessOpsChannelMapping = map[int]AccessOpsChannelMapping

// Alias for Map of AccessOperation -> Channel
type AccessOpsChannelMapping = map[AccessOperation][]chan interface{}

func WaitForAllSignalsForTx(messageIndexToAccessOpsChannelMapping MessageAccessOpsChannelMapping) {
	for _, accessOpsToChannelsMap := range messageIndexToAccessOpsChannelMapping {
		for _, channels := range accessOpsToChannelsMap {
			for _, channel := range channels {
				<-channel
			}
		}
	}
}

func SendAllSignalsForTx(messageIndexToAccessOpsChannelMapping MessageAccessOpsChannelMapping) {
	for _, accessOpsToChannelsMap := range messageIndexToAccessOpsChannelMapping {
		for _, channels := range accessOpsToChannelsMap {
			for _, channel := range channels {
				channel <- struct{}{}
			}
		}
	}
}

func GetMessageAccessOps(
	messageIndex int,
	messageAccessOpsChannelMapping MessageAccessOpsChannelMapping,
) []AccessOperation {
	accessOps := []AccessOperation{}

	for accessOp, _ := range messageAccessOpsChannelMapping[messageIndex] {
		accessOps = append(accessOps, accessOp)
	}
	return accessOps
}

func (a *AccessOperation) GetString() {
	fmt.Printf("ResourceType=%s, AccessType=%s, IdentifierTemplate=%s", a.GetResourceType(), a.GetAccessType(), a.GetIdentifierTemplate())
}
