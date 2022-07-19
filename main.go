package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	badger "github.com/dgraph-io/badger/v3"
)

type Config struct {
	BotToken           string              `json:"botToken"`
	FixedTrainingTimes []FixedTrainingTime `json:"fixedTrainingTimes"`
}

var config Config

func init() {
	configFile, err := os.Open("config.json")
	if err != nil {
	}

	bytes, err := ioutil.ReadAll(configFile)
	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		log.Fatal(err)
	}

	defer configFile.Close()
}

func main() {
	eventDB, err := badger.Open(badger.DefaultOptions("event_db"))
	if err != nil {
		log.Fatal(err)
	}

	handler := Handler{
		eventDB: eventDB,
		config:  config,
	}

	discord := Discord{}
	discord.start(config.BotToken, handler)

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	discord.stop()
	eventDB.Close()
}
