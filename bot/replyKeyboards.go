package bot

import (
	tb "gopkg.in/tucnak/telebot.v2"
)

func ReplyKeyboard(buttons []string) *tb.ReplyMarkup {
	kb := &tb.ReplyMarkup{ResizeReplyKeyboard: true, OneTimeKeyboard: true}
	buttonsCreated := []tb.Row{}
	for _, label := range buttons {
		buttonsCreated = append(buttonsCreated, kb.Row(kb.Text(label)))
	}
	kb.Reply(buttonsCreated...)
	return kb
}