package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type Discord struct {
	session *discordgo.Session
}

func (d *Discord) start(token string, handler Handler) error {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return err
	}

	d.session = dg

	handler.setup(dg)

	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return err
	}

	return nil
}

func (d *Discord) stop() error {
	return d.session.Close()
}
