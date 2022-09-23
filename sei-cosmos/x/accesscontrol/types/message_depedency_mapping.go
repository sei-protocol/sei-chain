package types

func (m *MessageDependencyMapping) GetMessageKey() string {
	return m.GetModuleName() + "_" + m.GetMessageType().String()
}
