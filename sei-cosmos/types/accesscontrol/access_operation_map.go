package accesscontrol

// Alias for Map of MessageIndex -> AccessOperation -> Channel
type MessageAccessOpsChannelMapping = map[int]AccessOpsChannelMapping

// Alias for Map of AccessOperation -> Channel
type AccessOpsChannelMapping = map[AccessOperation][]chan interface{}

func WaitForAllSignals(accessOpsToChannelsMap AccessOpsChannelMapping) {
	for _, channels := range accessOpsToChannelsMap {
		for _, channel := range channels {
			<-channel
		}
	}
}

func SendAllSignals(accessOpsToChannelsMap AccessOpsChannelMapping) {
	for _, channels := range accessOpsToChannelsMap {
		for _, channel := range channels {
			channel <- struct{}{}
		}
	}
}
