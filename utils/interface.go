package utils

type IAsset interface {
	IsCW20() bool
	IsCW721() bool
	IsNative() bool
	GetContractAddress() string
	GetDenom() string
}
