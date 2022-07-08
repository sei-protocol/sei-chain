package types

var OPPOSITE_POSITION_DIRECTION = map[PositionDirection]PositionDirection{
	PositionDirection_LONG:  PositionDirection_SHORT,
	PositionDirection_SHORT: PositionDirection_LONG,
}

var OPPOSITE_POSITION_EFFECT = map[PositionEffect]PositionEffect{
	PositionEffect_OPEN:  PositionEffect_CLOSE,
	PositionEffect_CLOSE: PositionEffect_OPEN,
}
