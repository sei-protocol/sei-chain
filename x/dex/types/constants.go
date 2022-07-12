package types

const (
	OpenOrderCreatorSuffix  = "o"
	CloseOrderCreatorSuffix = "c"
)

var SuffixToPositionEffect = map[string]PositionEffect{
	OpenOrderCreatorSuffix:  PositionEffect_OPEN,
	CloseOrderCreatorSuffix: PositionEffect_CLOSE,
}

var PositionEffectToSuffix = map[PositionEffect]string{
	PositionEffect_OPEN:  OpenOrderCreatorSuffix,
	PositionEffect_CLOSE: CloseOrderCreatorSuffix,
}

const FormattedAccountDelimiter = "|"
