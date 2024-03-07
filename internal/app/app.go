package app

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	runtime "github.com/banzaicloud/logrus-runtime-formatter"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	// nolint:gosec // pprof path is only exposed over localhost
	_ "net/http/pprof"
)

var (
	ErrAppInit = errors.New("error initializing app")
)

const (
	ProfilingEndpoint = "localhost:9091"
)

// Config holds configuration data when running mctl
// App holds attributes for the mtl application
type App struct {
	// Viper loads configuration parameters.
	v *viper.Viper
	// flipflop configuration.
	Config *Configuration
	// Logger is the app logger
	Logger *logrus.Logger
}

// New returns returns a new instance of the flipflop app
func New(storeKind model.StoreKind, cfgFile, loglevel string, profiling bool) (*App, <-chan os.Signal, error) {

	app := &App{
		v:      viper.New(),
		Config: &Configuration{},
		Logger: logrus.New(),
	}

	if err := app.LoadConfiguration(cfgFile, storeKind); err != nil {
		return nil, nil, err
	}

	switch model.LogLevel(loglevel) {
	case model.LogLevelDebug:
		app.Logger.Level = logrus.DebugLevel
	case model.LogLevelTrace:
		app.Logger.Level = logrus.TraceLevel
	default:
		app.Logger.Level = logrus.InfoLevel
	}

	runtimeFormatter := &runtime.Formatter{
		ChildFormatter: &logrus.JSONFormatter{},
		File:           true,
		Line:           true,
		BaseNameOnly:   true,
	}

	app.Logger.SetFormatter(runtimeFormatter)

	termCh := make(chan os.Signal, 1)

	// register for SIGINT, SIGTERM
	signal.Notify(termCh, syscall.SIGINT, syscall.SIGTERM)

	if profiling {
		enableProfilingEndpoint()
	}

	return app, termCh, nil
}

// enableProfilingEndpoint enables the profiling endpoint
func enableProfilingEndpoint() {
	go func() {
		server := &http.Server{
			Addr:              ProfilingEndpoint,
			ReadHeaderTimeout: 2 * time.Second, // nolint:gomnd // time duration value is clear as is.
		}

		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	log.Println("profiling enabled: " + ProfilingEndpoint + "/debug/pprof")
}
