package logger

type Logger interface {
	Printf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Fatal(args ...interface{})
}

type NilLogger struct{}

func (_ *NilLogger) Printf(format string, args ...interface{}) {}
func (_ *NilLogger) Debugf(format string, args ...interface{}) {}
func (_ *NilLogger) Infof(format string, args ...interface{})  {}
func (_ *NilLogger) Warnf(format string, args ...interface{})  {}
func (_ *NilLogger) Errorf(format string, args ...interface{}) {}
func (_ *NilLogger) Fatalf(format string, args ...interface{}) {}
func (_ *NilLogger) Debug(args ...interface{})                 {}
func (_ *NilLogger) Info(args ...interface{})                  {}
func (_ *NilLogger) Warn(args ...interface{})                  {}
func (_ *NilLogger) Error(args ...interface{})                 {}
func (_ *NilLogger) Fatal(args ...interface{})                 {}
