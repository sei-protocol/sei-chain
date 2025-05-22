package utils

type IAsset interface {
	IsCW20() bool
	IsCW721() bool
	GetContractAddress() string
}
