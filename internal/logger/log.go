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

func (*NilLogger) Printf(_ string, _ ...interface{}) {}
func (*NilLogger) Debugf(_ string, _ ...interface{}) {}
func (*NilLogger) Infof(_ string, _ ...interface{})  {}
func (*NilLogger) Warnf(_ string, _ ...interface{})  {}
func (*NilLogger) Errorf(_ string, _ ...interface{}) {}
func (*NilLogger) Fatalf(_ string, _ ...interface{}) {}
func (*NilLogger) Debug(_ ...interface{})            {}
func (*NilLogger) Info(_ ...interface{})             {}
func (*NilLogger) Warn(_ ...interface{})             {}
func (*NilLogger) Error(_ ...interface{})            {}
func (*NilLogger) Fatal(_ ...interface{})            {}
