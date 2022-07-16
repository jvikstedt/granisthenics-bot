package main

import "github.com/bwmarrin/discordgo"

type Handler struct {
}

func (h *Handler) setup(s *discordgo.Session) {
	s.AddHandler(h.messageCreate)
	s.Identify.Intents = discordgo.IntentsGuildMessages
}

func (h *Handler) messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "ping" {
		s.ChannelMessageSend(m.ChannelID, "Pong!")
	}

	if m.Content == "pong" {
		s.ChannelMessageSend(m.ChannelID, "Ping!")
	}
}
