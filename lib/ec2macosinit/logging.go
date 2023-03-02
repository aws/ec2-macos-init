package ec2macosinit

import (
	"fmt"
	"log"
	"log/syslog"
	"os"
)

// Logger contains booleans for where to log, a tag used in syslog and the syslog Writer itself.
type Logger struct {
	LogToStdout    bool
	LogToSystemLog bool
	Tag            string
	SystemLog      *syslog.Writer
}

// NewLogger creates a new logger. Logger writes using the LOG_LOCAL0 facility by default if system logging is enabled.
func NewLogger(tag string, systemLog bool, stdout bool) (logger *Logger, err error) {
	// Set up system logging, if enabled
	syslogger := &syslog.Writer{}
	if systemLog {
		syslogger, err = syslog.New(syslog.LOG_LOCAL0, tag)
		if err != nil {
			return &Logger{}, fmt.Errorf("ec2macosinit: unable to create new syslog logger: %s\n", err)
		}
	}
	// Set log to use microseconds, if stdout is enabled
	if stdout {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	return &Logger{LogToSystemLog: systemLog, LogToStdout: stdout, Tag: tag, SystemLog: syslogger}, nil
}

// Info writes info to stdout and/or the system log.
func (l *Logger) Info(v ...interface{}) {
	if l.LogToStdout {
		log.Print(v...)
	}
	if l.LogToSystemLog {
		_ = l.SystemLog.Info(fmt.Sprint(v...))
	}
}

// Infof writes formatted info to stdout and/or the system log.
func (l *Logger) Infof(format string, v ...interface{}) {
	if l.LogToStdout {
		log.Printf(format, v...)
	}
	if l.LogToSystemLog {
		_ = l.SystemLog.Info(fmt.Sprintf(format, v...))
	}
}

// Warn writes a warning to stdout and/or the system log.
func (l *Logger) Warn(v ...interface{}) {
	if l.LogToStdout {
		log.Print(v...)
	}
	if l.LogToSystemLog {
		_ = l.SystemLog.Warning(fmt.Sprint(v...))
	}
}

// Warnf writes a formatted warning to stdout and/or the system log.
func (l *Logger) Warnf(format string, v ...interface{}) {
	if l.LogToStdout {
		log.Printf(format, v...)
	}
	if l.LogToSystemLog {
		_ = l.SystemLog.Warning(fmt.Sprintf(format, v...))
	}
}

// Error writes an error to stdout and/or the system log.
func (l *Logger) Error(v ...interface{}) {
	if l.LogToStdout {
		log.Print(v...)
	}
	if l.LogToSystemLog {
		_ = l.SystemLog.Err(fmt.Sprint(v...))
	}
}

// Errorf writes a formatted error to stdout and/or the system log.
func (l *Logger) Errorf(format string, v ...interface{}) {
	if l.LogToStdout {
		log.Printf(format, v...)
	}
	if l.LogToSystemLog {
		_ = l.SystemLog.Err(fmt.Sprintf(format, v...))
	}
}

// Fatal writes an error to stdout and/or the system log then exits with requested code.
func (l *Logger) Fatal(e int, v ...interface{}) {
	l.Error(v...)
	os.Exit(e)
}

// Fatalf writes a formatted error to stdout and/or the system log then exits with requested code.
func (l *Logger) Fatalf(e int, format string, v ...interface{}) {
	l.Errorf(format, v...)
	os.Exit(e)
}
