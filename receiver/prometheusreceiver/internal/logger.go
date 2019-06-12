package internal

import "go.uber.org/zap"
import gokitLog "github.com/go-kit/kit/log"

func NewZapToGokitLogAdapter(logger *zap.Logger) *zapToGokitLogAdapter {
	// need to skip two levels in order to get the correct caller
	// one for this method, the other for gokitLog
	logger = logger.WithOptions(zap.AddCallerSkip(2))
	return  &zapToGokitLogAdapter{l: logger.Sugar()}
}

type zapToGokitLogAdapter struct {
	l  *zap.SugaredLogger
}

func (w *zapToGokitLogAdapter) Log(keyvals ...interface{}) error {
	if len(keyvals) % 2 == 0 {
		// expecting key value pairs, the number of items need to be even
		w.l.Infow("", keyvals...)
	} else {
		// in case something goes wrong
		w.l.Info(keyvals...)
	}
	return nil
}

var _ gokitLog.Logger = (*zapToGokitLogAdapter)(nil)
