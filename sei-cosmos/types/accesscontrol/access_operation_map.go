package accesscontrol

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
