package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

type fileHook struct {
	file      io.Writer
	formatter logrus.Formatter
	level     logrus.Level

	truncateAtNewLine bool
}

func newFileHook(file io.Writer, level logrus.Level, formatter logrus.Formatter) *fileHook {
	return &fileHook{
		file:      file,
		formatter: formatter,
		level:     level,
	}
}

func newFileHookWithNewlineTruncate(file io.Writer, level logrus.Level, formatter logrus.Formatter) *fileHook {
	f := newFileHook(file, level, formatter)
	f.truncateAtNewLine = true
	return f
}

func (h fileHook) Levels() []logrus.Level {
	var levels []logrus.Level
	for _, level := range logrus.AllLevels {
		if level <= h.level {
			levels = append(levels, level)
		}
	}

	return levels
}

func (h *fileHook) Fire(entry *logrus.Entry) error {
	// logrus reuses the same entry for each invocation of hooks.
	// so we need to make sure we leave them message field as we received.
	orig := entry.Message
	defer func() { entry.Message = orig }()

	msgs := []string{orig}
	if h.truncateAtNewLine {
		msgs = strings.Split(orig, "\n")
	}

	for _, msg := range msgs {
		// this makes it easier to call format on entry
		// easy without creating a new one for each split message.
		entry.Message = msg
		line, err := h.formatter.Format(entry)
		if err != nil {
			return err
		}

		if _, err := h.file.Write(line); err != nil {
			return err
		}
	}

	return nil
}

func setupFileHook(baseDir string) (func(), *os.File) {
	if baseDir != "" && baseDir != "." {
		if err := os.MkdirAll(baseDir, 0750); err != nil {
			logrus.Fatalf("failed to create base directory for logs: %v", err)
		}
	}
	logPath := filepath.Join(baseDir, ".oc-mirror.log")
	logfile, err := os.OpenFile(filepath.Clean(logPath), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		logrus.Fatalf("failed to open log file: %v", err)
	}

	originalHooks := logrus.LevelHooks{}
	for k, v := range logrus.StandardLogger().Hooks {
		originalHooks[k] = v
	}
	logrus.AddHook(newFileHook(logfile, logrus.TraceLevel, &logrus.TextFormatter{
		DisableColors:          true,
		DisableTimestamp:       false,
		FullTimestamp:          true,
		DisableLevelTruncation: false,
	}))

	return func() {
		if err := logfile.Close(); err != nil {
			logrus.Error(err)
		}
		logrus.StandardLogger().ReplaceHooks(originalHooks)
	}, logfile
}
