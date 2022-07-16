package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	badger "github.com/dgraph-io/badger/v3"
)

var (
	Token      string
	InviteCode string
)

func init() {
	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.StringVar(&InviteCode, "ic", "", "Invite code")
	flag.Parse()
}

func main() {
	eventDB, err := badger.Open(badger.DefaultOptions("/tmp/event_db"))
	if err != nil {
		log.Fatal(err)
	}

	handler := Handler{
		inviteCode: InviteCode,
		eventDB:    eventDB,
	}

	discord := Discord{}
	discord.start(Token, handler)

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	discord.stop()
	eventDB.Close()
}
