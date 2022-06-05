package types

var OPPOSITE_POSITION_DIRECTION = map[PositionDirection]PositionDirection{
	PositionDirection_LONG:  PositionDirection_SHORT,
	PositionDirection_SHORT: PositionDirection_LONG,
}
