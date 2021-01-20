package logger

import (
	"os"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
)

func NewLogger() log.Logger {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	return log.With(logger, "ts", log.TimestampFormat(func() time.Time { return time.Now().UTC() }, "Jan 02 15:04:05.99"), "caller", log.DefaultCaller)
}

func ApplyFilter(l string, logger log.Logger) (log.Logger, error) {
	var lvl level.Option
	switch l {
	case "error":
		lvl = level.AllowError()
	case "warn":
		lvl = level.AllowWarn()
	case "info":
		lvl = level.AllowInfo()
	case "debug":
		lvl = level.AllowDebug()
	default:
		return nil, errors.Errorf("unexpected log level:%v", l)
	}

	return level.NewFilter(logger, lvl), nil
}
