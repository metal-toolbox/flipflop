package flipflop

import "errors"

var (
	errInvalideConditionParams = errors.New("invalid condition parameters")
	errTaskConv                = errors.New("error in generic Task conversion")
	errUnsupportedAction       = errors.New("unsupported action")
)
