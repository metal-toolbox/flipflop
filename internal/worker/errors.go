package worker

import "errors"

var (
	pkgName = "internal/worker"

	errInvalideConditionParams = errors.New("invalid condition parameters")
	errTaskConv                = errors.New("error in generic Task conversion")
	errUnsupportedAction       = errors.New("unsupported action")
)
