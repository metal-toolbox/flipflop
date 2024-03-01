package fleetdb

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// stageLabel is the label included in all metrics collected by the fleetdb store
	stageLabel = prometheus.Labels{"stage": "fleetdb"}
)

func init() {

}
