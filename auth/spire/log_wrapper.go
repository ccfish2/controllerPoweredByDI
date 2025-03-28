package spire

import "github.com/sirupsen/logrus"

type spiffeLogWrapper struct {
	log logrus.FieldLogger
}

func newSpiffeLogWrapper(logger logrus.FieldLogger) *spiffeLogWrapper {
	return &spiffeLogWrapper{
		log: logger,
	}
}

func (l *spiffeLogWrapper) Debugf(format string, args ...interface{}) {
	l.log.Debugf(format, args...)
}

func (l *spiffeLogWrapper) Infof(format string, args ...interface{}) {
	l.log.Infof(format, args...)
}

func (l *spiffeLogWrapper) Warnf(format string, args ...interface{}) {
	l.log.Warnf(format, args...)
}

func (l *spiffeLogWrapper) Errorf(format string, args ...interface{}) {
	l.log.Warnf(format, args...)
}
