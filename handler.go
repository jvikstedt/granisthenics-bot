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

type WeekEvent struct {
	EventID     string    `json:"eventID"`
	Users       []string  `json:"users"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
}

type Week struct {
	Year   int          `json:"year"`
	Week   int          `json:"week"`
	Events []*WeekEvent `json:"events"`
}

func (w *Week) genKey() []byte {
	return []byte(fmt.Sprintf("%d-%d", w.Year, w.Week))
}

func (w *Week) genVal() []byte {
	b, _ := json.Marshal(w)
	return b
}

type Handler struct {
	config  Config
	eventDB *badger.DB
}

func (h *Handler) setup(s *discordgo.Session) {
	s.AddHandler(h.messageCreate)
	s.AddHandler(h.userJoinedEvent)
	s.AddHandler(h.userLeftEvent)
	s.AddHandler(h.ready)
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentGuildScheduledEvents
}

func (h *Handler) ready(s *discordgo.Session, r *discordgo.Ready) {

	for _, guild := range s.State.Guilds {
		events, err := s.GuildScheduledEvents(guild.ID, false)
		if err != nil {
			fmt.Printf("Failed to get event: %v", err)
			continue
		}

		for _, trainingTime := range h.config.FixedTrainingTimes {
			found := false
			for _, evt := range events {
				if evt.Name == trainingTime.Name {
					found = true
				}
			}

			if !found {
				channels, err := s.GuildChannels(guild.ID)
				if err != nil {
					fmt.Printf("Failed to get channels: %v", err)
					continue
				}
				generalChannel, ok := lo.Find(channels, func(c *discordgo.Channel) bool {
					return c.Name == "general"
				})

				if !ok {
					fmt.Printf("Failed to find channel by name general")
					return
				}

				targetDate, err := findDateByWeekday(time.Now(), trainingTime.WeekDay)
				if err != nil {
					fmt.Printf("%v", err)
					return
				}

				t1 := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), trainingTime.StartTimeHours, trainingTime.StartTimeMinutes, 0, targetDate.Nanosecond(), targetDate.Location())
				t2 := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), trainingTime.EndTimeHours, trainingTime.EndTimeMinutes, 0, targetDate.Nanosecond(), targetDate.Location())

				h.createScheduledEvent(s, guild.ID, generalChannel.ID, trainingTime.Name, trainingTime.Description, trainingTime.Location, t1, t2)
			}
		}
	}
}

func findDateByWeekday(date time.Time, targetWeekday int) (time.Time, error) {
	if targetWeekday < 0 || targetWeekday > 6 {
		return date, fmt.Errorf("targetWeekday must be between 0 and 6, was %d", targetWeekday)
	}
	if int(date.Weekday()) == targetWeekday {
		return date, nil
	}

	return findDateByWeekday(date.Add(24*time.Hour), targetWeekday)
}

func (h *Handler) userJoinedEvent(s *discordgo.Session, m *discordgo.GuildScheduledEventUserAdd) {
	event, err := s.GuildScheduledEvent(m.GuildID, m.GuildScheduledEventID, false)
	if err != nil {
		fmt.Printf("Failed to get event: %v", err)
		return
	}

	y, w := event.ScheduledStartTime.ISOWeek()
	key := []byte(fmt.Sprintf("%s-%d-%d", m.GuildID, y, w))

	err = h.eventDB.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		week := Week{}
		err = item.Value(func(v []byte) error {
			return json.Unmarshal(v, &week)
		})
		if err != nil {
			fmt.Printf("Error unmarshalling data: %v", err)
			return err
		}

		for _, evt := range week.Events {
			if evt.EventID == event.ID {
				evt.Users = append(evt.Users, m.UserID)
			}
		}

		txn.SetEntry(badger.NewEntry(key, week.genVal()))

		return nil
	})

	if err != nil {
		fmt.Printf("Error updating db: %v", err)
		return
	}
}

func (h *Handler) userLeftEvent(s *discordgo.Session, m *discordgo.GuildScheduledEventUserRemove) {
	event, err := s.GuildScheduledEvent(m.GuildID, m.GuildScheduledEventID, false)
	if err != nil {
		fmt.Printf("Failed to get event: %v", err)
	}

	y, w := event.ScheduledStartTime.ISOWeek()
	key := []byte(fmt.Sprintf("%s-%d-%d", m.GuildID, y, w))

	err = h.eventDB.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		week := Week{}
		err = item.Value(func(v []byte) error {
			return json.Unmarshal(v, &week)
		})
		if err != nil {
			fmt.Printf("Error unmarshalling data: %v", err)
			return err
		}

		for _, evt := range week.Events {
			if evt.EventID == event.ID {
				for id, userID := range evt.Users {
					if userID == m.UserID {
						evt.Users = append(evt.Users[:id], evt.Users[id+1:]...)
						continue
					}
				}
			}
		}

		txn.SetEntry(badger.NewEntry(key, week.genVal()))

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
		startingTime := time.Now().Add(1 * time.Hour)
		endingTime := startingTime.Add(30 * time.Minute)

		h.createScheduledEvent(s, m.GuildID, m.ChannelID, "Treeni", "Mennään metsään", "Kasavuori", startingTime, endingTime)
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

func (h *Handler) createScheduledEvent(s *discordgo.Session, guildID, channelID, name, description, location string, startingTime, endingTime time.Time) {
	event, err := s.GuildScheduledEventCreate(guildID, &discordgo.GuildScheduledEventParams{
		Name:               name,
		Description:        description,
		ScheduledStartTime: &startingTime,
		ScheduledEndTime:   &endingTime,
		EntityType:         discordgo.GuildScheduledEventEntityTypeExternal,
		EntityMetadata: &discordgo.GuildScheduledEventEntityMetadata{
			Location: location,
		},
		PrivacyLevel: discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
	})

	if err != nil {
		fmt.Printf("Error creating scheduled event: %v", err)
		return
	}

	invites, err := s.ChannelInvites(channelID)
	if err != nil {
		fmt.Printf("Error getting invites: %v", err)
		return
	}

	inviteCode := ""

	for _, invite := range invites {
		if !invite.Temporary {
			inviteCode = invite.Code
		}
	}

	if inviteCode == "" {
		fmt.Println("Could not find invite code")
	} else {
		s.ChannelMessageSend(channelID, fmt.Sprintf("https://discord.gg/%s?event=%s", inviteCode, event.ID))
	}

	y, w := startingTime.ISOWeek()
	key := []byte(fmt.Sprintf("%s-%d-%d", guildID, y, w))

	err = h.eventDB.Update(func(txn *badger.Txn) error {
		week := Week{
			Year:   y,
			Week:   w,
			Events: nil,
		}

		item, err := txn.Get(key)
		if err == nil {
			err = item.Value(func(v []byte) error {
				return json.Unmarshal(v, &week)
			})
			if err != nil {
				fmt.Printf("Error unmarshalling data: %v", err)
				return err
			}
		}

		week.Events = append(week.Events, &WeekEvent{
			EventID:     event.ID,
			Users:       []string{},
			Name:        event.Name,
			Description: event.Description,
			Location:    event.EntityMetadata.Location,
			StartTime:   event.ScheduledStartTime,
			EndTime:     *event.ScheduledEndTime,
		})

		txn.SetEntry(badger.NewEntry(key, week.genVal()))
		return nil
	})

	if err != nil {
		fmt.Printf("Error updating db: %v", err)
		return
	}
}
