package logs

import (
	"github.com/labstack/gommon/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
)

func NewZap(prefix string, level string) (*Zap, error) {
	var logger *zap.Logger
	lvl := zap.NewAtomicLevel()
	err := lvl.UnmarshalText([]byte(level))
	if err != nil {
		return nil, err
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = lvl
	logger, err = cfg.Build()
	if err != nil {
		return nil, err
	}
	return &Zap{
		SugaredLogger: logger.Sugar(),
		level:         &lvl,
		prefix:        prefix,
	}, err
}

type Zap struct {
	*zap.SugaredLogger
	level         *zap.AtomicLevel
	prefix        string
	writer        io.Writer
	requestFields []zapcore.Field
}

func (z *Zap) WithRequestID(prefix string, level string, requestID string) (*Zap, error) {

	var logger *zap.Logger
	lvl := zap.NewAtomicLevel()
	err := lvl.UnmarshalText([]byte(level))
	if err != nil {
		return nil, err
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = lvl
	logger, err = cfg.Build()
	if err != nil {
		return nil, err
	}
	z.SugaredLogger = logger.Sugar().Named(prefix).With(zap.String("request_id", requestID))
	z.level = &lvl
	z.prefix = prefix
	return z, err
}

func (z *Zap) Printf(format string, args ...interface{}) {
	z.Infof(format, args...)
}

func (z *Zap) Print(args ...interface{}) {
	z.Info(args...)
}

func (z *Zap) Output() io.Writer {
	//return zapcore.AddSync(os.Stdout)
	return z.writer
}

func (z *Zap) SetOutput(w io.Writer) {
	z.writer = w
	//core := zapcore.NewCore(
	//	zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
	//	zapcore.AddSync(w),
	//	zap.NewAtomicLevelAt(z.level.Level()),
	//)
	//logger := zap.New(core)
	//z.SugaredLogger = logger.Sugar()
}

func (z *Zap) Prefix() string {
	return z.prefix
}

func (z *Zap) SetPrefix(p string) {
	z.prefix = p
	z.SugaredLogger = z.Named(p)
}

func (z *Zap) Level() log.Lvl {
	switch z.level.Level() {
	case zapcore.DebugLevel:
		return log.DEBUG
	case zapcore.InfoLevel:
		return log.INFO
	case zapcore.WarnLevel:
		return log.WARN
	case zapcore.ErrorLevel, zapcore.FatalLevel, zapcore.PanicLevel:
		return log.ERROR
	}
	return log.OFF
}

func (z *Zap) SetLevel(v log.Lvl) {
	switch v {
	case log.DEBUG:
		z.level.SetLevel(zapcore.DebugLevel)
	case log.INFO:
		z.level.SetLevel(zapcore.InfoLevel)
	case log.WARN:
		z.level.SetLevel(zapcore.WarnLevel)
	case log.ERROR:
		z.level.SetLevel(zapcore.ErrorLevel)
	}

}

func (z *Zap) SetHeader(h string) {
	z.Warnf("configuring the log template should be done on instantiation")
}

func (z *Zap) Printj(j log.JSON) {
	z.Info(j)
}

func (z *Zap) Debugj(j log.JSON) {
	z.Debug(j)
}

func (z *Zap) Infoj(j log.JSON) {
	z.Info(j)
}

func (z *Zap) Warnj(j log.JSON) {
	z.Warn(j)
}

func (z *Zap) Errorj(j log.JSON) {
	z.Error(j)
}

func (z *Zap) Fatalj(j log.JSON) {
	z.Fatal(j)
}

func (z *Zap) Panicj(j log.JSON) {
	z.Panic(j)
}

