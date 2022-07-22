package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	badger "github.com/dgraph-io/badger/v3"
	"github.com/samber/lo"
)

type FixedTrainingTime struct {
	WeekDay          int    `json:"weekDay"`
	StartTimeHours   int    `json:"startTimeHours"`
	StartTimeMinutes int    `json:"startTimeMinutes"`
	EndTimeHours     int    `json:"endTimeHours"`
	EndTimeMinutes   int    `json:"endTimeMinutes"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Location         string `json:"location"`
}

type Event struct {
	MessageID   string    `json:"messageID"`
	AnswerYes   []string  `json:"answerYes"`
	AnswerNo    []string  `json:"answerNo"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
}

type Handler struct {
	config  Config
	eventDB *badger.DB
	botID   string
}

type Metadata struct {
	AllEvents         []string  `json:"allEvents"`
	CurrentWeekEvents []string  `json:"currentWeekEvents"`
	LastWeekReset     time.Time `json:"lastWeekReset"`
}

const REACTION_NO = "❌"
const REACTION_YES = "✅"

const KEY_METADATA = "METADATA"

func (h *Handler) setup(s *discordgo.Session) {
	s.AddHandler(h.messageCreate)
	s.AddHandler(h.ready)
	s.AddHandler(h.reactionAdd)
	s.AddHandler(h.reactionRemove)
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentGuildMessageReactions
	// s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentGuildScheduledEvents
}

func (h *Handler) ready(s *discordgo.Session, r *discordgo.Ready) {
	h.botID = r.User.ID
	h.createEventChannels(s)

	for _, guild := range s.State.Guilds {
		// Create metadata if does not exist
		key := []byte(fmt.Sprintf("%s-%s", guild.ID, KEY_METADATA))
		err := h.eventDB.Update(func(txn *badger.Txn) error {
			_, err := txn.Get(key)

			if err == badger.ErrKeyNotFound {
				metadata := Metadata{
					AllEvents:         []string{},
					CurrentWeekEvents: []string{},
				}
				bytes, err := json.Marshal(&metadata)
				if err != nil {
					return err
				}
				txn.SetEntry(badger.NewEntry(key, bytes))
			}
			return nil
		})
		if err != nil {
			fmt.Printf("Error checking/creating metadata %v\n", err)
		}
	}

}

func (h *Handler) reactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	// Skip bots own reactions
	if r.UserID == h.botID {
		return
	}

	msgKey := []byte(fmt.Sprintf("%s-%s", r.GuildID, r.MessageID))

	// Skip if reaction was not added on event message
	isEvent := false
	h.eventDB.View(func(txn *badger.Txn) error {
		item, _ := txn.Get(msgKey)
		isEvent = item != nil
		return nil
	})
	if !isEvent {
		return
	}

	// Only allow yes or no reactions, remove otherwise
	if r.Emoji.Name != REACTION_NO && r.Emoji.Name != REACTION_YES {
		if err := s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.Name, r.UserID); err != nil {
			fmt.Printf("Could not remove reaction %v\n", err)
		}

		return
	}

	answerYes := r.Emoji.Name == REACTION_YES

	noReactions, err := s.MessageReactions(r.ChannelID, r.MessageID, REACTION_NO, 100, "", "")
	if err != nil {
		fmt.Printf("Could not get no reactions %v\n", err)
		return
	}

	yesReactions, err := s.MessageReactions(r.ChannelID, r.MessageID, REACTION_YES, 100, "", "")
	if err != nil {
		fmt.Printf("Could not get yes reactions %v\n", err)
		return
	}

	// Only allow one reaction at the time
	if answerYes {
		_, existingNo := lo.Find(noReactions, func(u *discordgo.User) bool {
			return u.ID == r.UserID
		})
		if existingNo {
			if err := s.MessageReactionRemove(r.ChannelID, r.MessageID, REACTION_NO, r.UserID); err != nil {
				fmt.Printf("Could not remove reaction %v\n", err)
			}
		}
	} else {
		_, existingYes := lo.Find(yesReactions, func(u *discordgo.User) bool {
			return u.ID == r.UserID
		})
		if existingYes {
			if err := s.MessageReactionRemove(r.ChannelID, r.MessageID, REACTION_YES, r.UserID); err != nil {
				fmt.Printf("Could not remove reaction %v\n", err)
			}
		}
	}

	err = h.eventDB.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(msgKey)
		if err != nil {
			return err
		}
		event := Event{}
		err = item.Value(func(v []byte) error {
			return json.Unmarshal(v, &event)
		})
		if err != nil {
			fmt.Printf("Error unmarshalling data: %v", err)
			return err
		}

		// Update reactions
		event.AnswerYes = lo.Map(yesReactions, func(u *discordgo.User, _ int) string {
			return u.ID
		})
		event.AnswerYes = lo.Reject(event.AnswerYes, func(userID string, _ int) bool {
			return userID == h.botID
		})
		event.AnswerNo = lo.Map(noReactions, func(u *discordgo.User, _ int) string {
			return u.ID
		})
		event.AnswerNo = lo.Reject(event.AnswerNo, func(userID string, _ int) bool {
			return userID == h.botID
		})

		bytes, err := json.Marshal(event)
		if err != nil {
			return nil
		}

		txn.SetEntry(badger.NewEntry(msgKey, bytes))

		return nil
	})

	if err != nil {
		fmt.Printf("Error adding reaction: %v", err)
	}
}

func (h *Handler) reactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	// Skip bots own reactions
	if r.UserID == h.botID {
		return
	}

	msgKey := []byte(fmt.Sprintf("%s-%s", r.GuildID, r.MessageID))

	// Skip if reaction was not added on event message
	isEvent := false
	h.eventDB.View(func(txn *badger.Txn) error {
		item, _ := txn.Get(msgKey)
		isEvent = item != nil
		return nil
	})
	if !isEvent {
		return
	}

	// Only allow yes or no reactions, remove otherwise
	if r.Emoji.Name != REACTION_NO && r.Emoji.Name != REACTION_YES {
		if err := s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.Name, r.UserID); err != nil {
			fmt.Printf("Could not remove reaction %v\n", err)
		}

		return
	}

	noReactions, err := s.MessageReactions(r.ChannelID, r.MessageID, REACTION_NO, 100, "", "")
	if err != nil {
		fmt.Printf("Could not get no reactions %v\n", err)
		return
	}

	yesReactions, err := s.MessageReactions(r.ChannelID, r.MessageID, REACTION_YES, 100, "", "")
	if err != nil {
		fmt.Printf("Could not get yes reactions %v\n", err)
		return
	}

	err = h.eventDB.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(msgKey)
		if err != nil {
			return err
		}
		event := Event{}
		err = item.Value(func(v []byte) error {
			return json.Unmarshal(v, &event)
		})
		if err != nil {
			fmt.Printf("Error unmarshalling data: %v", err)
			return err
		}

		event.AnswerYes = lo.Map(yesReactions, func(u *discordgo.User, _ int) string {
			return u.ID
		})
		event.AnswerYes = lo.Reject(event.AnswerYes, func(userID string, _ int) bool {
			return userID == h.botID
		})
		event.AnswerNo = lo.Map(noReactions, func(u *discordgo.User, _ int) string {
			return u.ID
		})
		event.AnswerNo = lo.Reject(event.AnswerNo, func(userID string, _ int) bool {
			return userID == h.botID
		})

		bytes, err := json.Marshal(event)
		if err != nil {
			return nil
		}

		txn.SetEntry(badger.NewEntry(msgKey, bytes))

		return nil
	})

	if err != nil {
		fmt.Printf("Error updating db: %v", err)
		return
	}
}

func (h *Handler) messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!event" {
		evt, err := h.createNewEvent(s, m.GuildID, "Reeni", "", "Kasavuori", time.Now(), time.Now().Add(1*time.Hour))
		if err != nil {
			fmt.Printf("Error creating an event: %v", err)
			return
		}

		fmt.Println(evt)
	}

	if m.Content == "!check" {
		h.check(s)
	}

	if m.Content == "!listEvents" {
		err := h.eventDB.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchSize = 10
			it := txn.NewIterator(opts)
			defer it.Close()
			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				k := item.Key()
				err := item.Value(func(v []byte) error {
					s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("key=%s, value=%s", k, v))
					return nil
				})
				if err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			fmt.Printf("Error viewing db: %v", err)
			return
		}
	}
}

func (h *Handler) findChannelByName(s *discordgo.Session, guildID string, name string) (*discordgo.Channel, error) {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return nil, err
	}
	channel, found := lo.Find(channels, func(c *discordgo.Channel) bool {
		return c.Name == name
	})

	if found {
		return channel, nil
	}

	return nil, fmt.Errorf("Could not find channel for guild: %s by name %s", guildID, name)
}

func (h *Handler) createEventChannels(s *discordgo.Session) error {
	for _, guild := range s.State.Guilds {
		if _, err := h.findChannelByName(s, guild.ID, h.config.ChannelName); err != nil {
			fmt.Printf("Channel: %s does not exist for Guild: %s, creating...\n", h.config.ChannelName, guild.Name)

			if _, err := s.GuildChannelCreate(guild.ID, h.config.ChannelName, discordgo.ChannelTypeGuildText); err != nil {
				fmt.Printf("Failed to create channel %v\n", err)
				continue
			}
		}
	}

	return nil
}

func (h *Handler) createNewEvent(s *discordgo.Session, guildID, name, description, location string, startTime, endTime time.Time) (*Event, error) {
	channel, err := h.findChannelByName(s, guildID, h.config.ChannelName)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("Europe/Helsinki")
	format := "2.1 15:04"

	msg, err := s.ChannelMessageSend(channel.ID, fmt.Sprintf("-------\n%s\n%s -> %s\n%s\n%s\n-------", name, startTime.In(loc).Format(format), endTime.In(loc).Format(format), description, location))
	if err != nil {
		return nil, err
	}

	if err := s.MessageReactionAdd(msg.ChannelID, msg.ID, REACTION_YES); err != nil {
		return nil, err
	}
	if err := s.MessageReactionAdd(msg.ChannelID, msg.ID, REACTION_NO); err != nil {
		return nil, err
	}

	event := &Event{
		MessageID:   msg.ID,
		AnswerYes:   []string{},
		AnswerNo:    []string{},
		Name:        name,
		Description: description,
		Location:    location,
		StartTime:   startTime,
		EndTime:     endTime,
	}

	bytes, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	metadata, err := h.getMetadata(guildID)
	if err != nil {
		fmt.Printf("metadata not found %s\n", guildID)
		return nil, err
	}

	metadata.CurrentWeekEvents = append(metadata.CurrentWeekEvents, msg.ID)
	metadata.AllEvents = append(metadata.AllEvents, msg.ID)
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	metadataKey := []byte(fmt.Sprintf("%s-%s", guildID, KEY_METADATA))
	msgKey := []byte(fmt.Sprintf("%s-%s", guildID, msg.ID))
	return event, h.eventDB.Update(func(txn *badger.Txn) error {
		txn.SetEntry(badger.NewEntry(metadataKey, metadataBytes))
		txn.SetEntry(badger.NewEntry(msgKey, bytes))
		return nil
	})
}

func (h *Handler) getMetadata(guildID string) (Metadata, error) {
	metadata := Metadata{}

	key := []byte(fmt.Sprintf("%s-%s", guildID, KEY_METADATA))

	err := h.eventDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		err = item.Value(func(v []byte) error {
			return json.Unmarshal(v, &metadata)
		})
		if err != nil {
			fmt.Printf("Error unmarshalling data: %v", err)
			return err
		}

		return nil
	})

	return metadata, err
}

func (h *Handler) check(s *discordgo.Session) {
	now := time.Now()

	for _, guild := range s.State.Guilds {
		metadataKey := []byte(fmt.Sprintf("%s-%s", guild.ID, KEY_METADATA))
		metadata, err := h.getMetadata(guild.ID)
		if err != nil {
			fmt.Printf("Error getting metadata %v\n", err)
			continue
		}

		// Is Monday and more than 7 days - 1 hour since last reset and its after 9 AM
		if time.Since(metadata.LastWeekReset) >= (time.Hour*(24*7-1)) && now.Weekday() == time.Monday {
			fmt.Printf("Resetting CurrentWeekEvents\n")
			metadata.LastWeekReset = time.Now()
			metadata.CurrentWeekEvents = []string{}

			metadataBytes, err := json.Marshal(metadata)
			if err != nil {
				fmt.Printf("Error marshalling metadata %v\n", err)
				return
			}

			err = h.eventDB.Update(func(txn *badger.Txn) error {
				txn.SetEntry(badger.NewEntry(metadataKey, metadataBytes))
				return nil
			})
			if err != nil {
				fmt.Printf("Error saving metadata %v\n", err)
				return
			}
		}

		currentWeekEvents := []Event{}

		err = h.eventDB.View(func(txn *badger.Txn) error {
			event := Event{}
			for _, eventID := range metadata.CurrentWeekEvents {
				item, err := txn.Get([]byte(fmt.Sprintf("%s-%s", guild.ID, eventID)))
				if err != nil {
					return err
				}

				err = item.Value(func(v []byte) error {
					return json.Unmarshal(v, &event)
				})
				if err != nil {
					fmt.Printf("Error unmarshalling data: %v", err)
					return err
				}

				currentWeekEvents = append(currentWeekEvents, event)
			}

			return nil
		})
		if err != nil {
			fmt.Printf("Error getting week events %v\n", err)
			continue
		}

		// Check fixed training times
		for _, trainingTime := range h.config.FixedTrainingTimes {
			if int(now.Weekday()) == trainingTime.WeekDay {
				t1 := time.Date(now.Year(), now.Month(), now.Day(), trainingTime.StartTimeHours, trainingTime.StartTimeMinutes, 0, now.Nanosecond(), now.Location())
				t2 := time.Date(now.Year(), now.Month(), now.Day(), trainingTime.EndTimeHours, trainingTime.EndTimeMinutes, 0, now.Nanosecond(), now.Location())

				if t1.Before(now) {
					continue
				}

				_, ok := lo.Find(currentWeekEvents, func(event Event) bool {
					return event.Name == trainingTime.Name &&
						int(event.StartTime.Weekday()) == trainingTime.WeekDay &&
						event.StartTime.Hour() == trainingTime.StartTimeHours &&
						event.StartTime.Minute() == trainingTime.StartTimeMinutes
				})

				if ok {
					continue
				}

				_, err := h.createNewEvent(s, guild.ID, trainingTime.Name, trainingTime.Description, trainingTime.Location, t1, t2)
				if err != nil {
					fmt.Printf("Error creating an event %v\n", err)
					continue
				}
			}
		}
	}
}
