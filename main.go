package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: false,
			Colorful:                  true,
		},
	)
	eventDB, err := gorm.Open(sqlite.Open("events.db"), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		panic("failed to connect database")
	}

	eventDB.AutoMigrate(&User{})
	eventDB.AutoMigrate(&Answer{})
	eventDB.AutoMigrate(&Event{})
	eventDB.AutoMigrate(&Metadata{})
	eventDB.AutoMigrate(&FixedTrainingTime{})

	repository := Repository{
		db: eventDB,
	}

	handler := Handler{
		repository: &repository,
		config:     config,
	}

	discord := Discord{}
	discord.start(config.BotToken, handler)

	go func() {
		for {
			time.Sleep(time.Second * 60)
			handler.check(discord.session)
		}
	}()

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	discord.stop()
}
