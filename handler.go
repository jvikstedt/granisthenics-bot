package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	badger "github.com/dgraph-io/badger/v3"
)

type FixedTrainingTime struct {
	WeekDay     string `json:"weekDay"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

type WeekEvent struct {
	EventID string   `json:"eventID"`
	Users   []string `json:"users"`
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
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentGuildScheduledEvents
}

func (h *Handler) userJoinedEvent(s *discordgo.Session, m *discordgo.GuildScheduledEventUserAdd) {
	event, err := s.GuildScheduledEvent(m.GuildID, m.GuildScheduledEventID, false)
	if err != nil {
		fmt.Printf("Failed to get event: %v", err)
	}

	y, w := event.ScheduledStartTime.ISOWeek()
	key := []byte(fmt.Sprintf("%d-%d", y, w))

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

		txn.SetEntry(badger.NewEntry(week.genKey(), week.genVal()))

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
	key := []byte(fmt.Sprintf("%d-%d", y, w))

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

		txn.SetEntry(badger.NewEntry(week.genKey(), week.genVal()))

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
		event, err := s.GuildScheduledEventCreate(m.GuildID, &discordgo.GuildScheduledEventParams{
			Name:               "Reeni",
			Description:        "Mennään metsään",
			ScheduledStartTime: &startingTime,
			ScheduledEndTime:   &endingTime,
			EntityType:         discordgo.GuildScheduledEventEntityTypeExternal,
			EntityMetadata: &discordgo.GuildScheduledEventEntityMetadata{
				Location: "Kasavuori",
			},
			PrivacyLevel: discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
		})

		if err != nil {
			fmt.Printf("Error creating scheduled event: %v", err)
			return
		}

		invites, err := s.ChannelInvites(m.ChannelID)
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
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("https://discord.gg/%s?event=%s", inviteCode, event.ID))
		}

		y, w := startingTime.ISOWeek()
		key := []byte(fmt.Sprintf("%d-%d", y, w))

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
				EventID: event.ID,
				Users:   []string{},
			})

			txn.SetEntry(badger.NewEntry(week.genKey(), week.genVal()))
			return nil
		})

		if err != nil {
			fmt.Printf("Error updating db: %v", err)
			return
		}
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
