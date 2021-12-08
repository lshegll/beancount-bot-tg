package helpers

import (
	"fmt"
	"log"
	"strconv"

	tb "gopkg.in/tucnak/telebot.v2"
)

type Level int

const (
	TRACE = Level(iota)
	DEBUG
	INFO
	WARN
	ERROR
	FATAL
)

func (l Level) String() string {
	levels := []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
	i := int(l)
	switch {
	case i <= int(FATAL):
		return levels[i]
	default:
		return strconv.Itoa(int(i))
	}
}

func LogLocalf(level Level, m *tb.Message, format string, v ...interface{}) string {
	msg := fmt.Sprintf("[%s] %s%s", level, LogMessagePrefix(m), fmt.Sprintf(format, v...))
	log.Print(msg)
	return msg
}

func LogMessagePrefix(m *tb.Message) string {
	prefix := ""
	if m != nil {
		// Account for historical test cases without sender
		if m.Sender == nil {
			m.Sender = &tb.User{ID: -1}
		}
		prefix = fmt.Sprintf("[C%d/U%d] ", m.Chat.ID, m.Sender.ID)
	}
	return prefix
}
