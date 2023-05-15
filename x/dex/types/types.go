package types

var OppositePositionDirection = map[PositionDirection]PositionDirection{
	PositionDirection_LONG:  PositionDirection_SHORT,
	PositionDirection_SHORT: PositionDirection_LONG,
}
