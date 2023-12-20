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

func (_ *NilLogger) Printf(_ string, _ ...interface{}) {}
func (_ *NilLogger) Debugf(_ string, _ ...interface{}) {}
func (_ *NilLogger) Infof(_ string, _ ...interface{})  {}
func (_ *NilLogger) Warnf(_ string, _ ...interface{})  {}
func (_ *NilLogger) Errorf(_ string, _ ...interface{}) {}
func (_ *NilLogger) Fatalf(_ string, _ ...interface{}) {}
func (_ *NilLogger) Debug(_ ...interface{})            {}
func (_ *NilLogger) Info(_ ...interface{})             {}
func (_ *NilLogger) Warn(_ ...interface{})             {}
func (_ *NilLogger) Error(_ ...interface{})            {}
func (_ *NilLogger) Fatal(_ ...interface{})            {}
