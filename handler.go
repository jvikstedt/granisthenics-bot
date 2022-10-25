package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/samber/lo"
)

type Handler struct {
	config     Config
	repository *Repository
	botID      string
}

const REACTION_NO = "❌"
const REACTION_YES = "✅"
const REACTION_MAYBE = "❔"

const KEY_METADATA = "METADATA"

func (h *Handler) setup(s *discordgo.Session) {
	s.AddHandler(h.messageCreate)
	s.AddHandler(h.ready)
	s.AddHandler(h.reactionAdd)
	s.Identify.Intents = discordgo.IntentsAll
}

func (h *Handler) ready(s *discordgo.Session, r *discordgo.Ready) {
	h.botID = r.User.ID

	for _, guild := range s.State.Guilds {
		_, err := h.repository.FindMetadataByGuildID(guild.ID)
		if errors.Is(err, ErrRecordNotFound) {
			// Create new
			metadata := Metadata{
				GuildID:       guild.ID,
				LastWeekReset: time.Time{},
				ChannelName:   "general",
			}

			createdMetadata, err := h.repository.CreateMetadata(metadata)
			if err != nil {
				fmt.Printf("creating metadata failed %v\n", err)
				continue
			}

			fmt.Printf("Created new metadata: %v", createdMetadata)
		} else if err != nil {
			fmt.Printf("Finding metadata by guild id failed %v\n", err)
			continue
		} else {
			// Do nothing
		}
	}

	h.createEventChannels(s)
}

func (h *Handler) reactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	// Skip bots own reactions
	if r.UserID == h.botID {
		return
	}

	event, err := h.repository.FindEvent(r.GuildID, r.MessageID)
	if err != nil {
		fmt.Printf("Could not get event %v\n", err)
		return
	}

	discordUser, err := s.User(r.UserID)
	if err != nil {
		fmt.Printf("Could not get discord user %v\n", err)
		return
	}

	user, err := h.repository.FindUser(r.GuildID, r.UserID)
	if err != nil && errors.Is(err, ErrRecordNotFound) {
		user, err = h.repository.CreateUser(User{
			GuildID:  r.GuildID,
			MemberID: r.UserID,
			Name:     discordUser.Username,
		})
	}

	if err != nil {
		fmt.Printf("Could not get user %v\n", err)
		return
	}

	answer, err := h.repository.FindAnswer(event.ID, user.ID)

	if err != nil && errors.Is(err, ErrRecordNotFound) {
		answer = Answer{
			YesNo:   reactionToYesNo(r.Emoji.Name),
			EventID: event.ID,
			UserID:  user.ID,
		}
	} else if err != nil {
		fmt.Printf("Could find answer %v\n", err)
		return
	}

	answer.YesNo = reactionToYesNo(r.Emoji.Name)

	_, err = h.repository.UpdateOrCreateAnswer(answer)
	if err != nil {
		fmt.Printf("Could not update/create answer %v\n", err)
		return
	}

	if err = h.updateEvent(s, r.GuildID, r.MessageID); err != nil {
		fmt.Printf("Could not update event %v\n", err)
		return
	}

	// Remove reaction
	if err := s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.Name, r.UserID); err != nil {
		fmt.Printf("Could not remove reaction %v\n", err)
		return
	}
}

func yesNoToReaction(yesno string) string {
	switch yesno {
	case "yes":
		return REACTION_YES
	case "no":
		return REACTION_NO
	default:
		return REACTION_MAYBE
	}
}

func reactionToYesNo(reaction string) string {
	switch reaction {
	case REACTION_YES:
		return "yes"
	case REACTION_NO:
		return "no"
	default:
		return "maybe"
	}
}

func (h *Handler) updateEvent(s *discordgo.Session, guildID string, messageID string) error {
	metadata, err := h.repository.FindMetadataByGuildID(guildID)
	if err != nil {
		return err
	}
	channel, err := h.findChannelByName(s, guildID, metadata.ChannelName)
	if err != nil {
		return err
	}

	event, err := h.repository.FindEvent(guildID, messageID)
	if err != nil {
		return err
	}

	users, err := h.repository.FindUsers(guildID)
	if err != nil {
		return err
	}

	loc, _ := time.LoadLocation("Europe/Helsinki")
	format := "2.1 15:04"
	answersSlice := lo.Map(event.Answers, func(a Answer, _ int) string {
		user, _ := lo.Find(users, func(u User) bool {
			return u.ID == a.UserID
		})

		return fmt.Sprintf("%s %s (%s)", yesNoToReaction(a.YesNo), user.Name, a.UpdatedAt.In(loc).Format(format))
	})

	_, err = s.ChannelMessageEdit(channel.ID, event.MessageID, buildEventContent(event.Name, event.StartTime, event.EndTime, event.Description, event.Location, strings.Join(answersSlice, "\n")))
	if err != nil {
		return err
	}

	return nil
}

func buildEventContent(name string, startTime time.Time, endTime time.Time, description string, location string, answers string) string {
	loc, _ := time.LoadLocation("Europe/Helsinki")
	format := "2.1 15:04"
	return fmt.Sprintf("━━━━━━━━━━\n**%s** :man_lifting_weights:\n%s -> %s\n%s\n%s\n%s\n@everyone\n━━━━━━━━━━", name, startTime.In(loc).Format(format), endTime.In(loc).Format(format), description, location, answers)
}

func (h *Handler) messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	permissions, err := s.State.MessagePermissions(m.Message)
	if err != nil {
		fmt.Printf("Could not get permissions for user %v\n", err)
		return
	}

	if permissions&discordgo.PermissionAdministrator == 0 {
		fmt.Println("Non admin user tried to execute commands")
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
		metadata, err := h.repository.FindMetadataByGuildID(guild.ID)
		if err != nil {
			continue
		}

		if _, err := h.findChannelByName(s, guild.ID, metadata.ChannelName); err != nil {
			fmt.Printf("Channel: %s does not exist for Guild: %s, creating...\n", metadata.ChannelName, guild.Name)

			if _, err := s.GuildChannelCreate(guild.ID, metadata.ChannelName, discordgo.ChannelTypeGuildText); err != nil {
				fmt.Printf("Failed to create channel %v\n", err)
				continue
			}
		}
	}

	return nil
}

func (h *Handler) createNewEvent(s *discordgo.Session, guildID, name, description, location string, startTime, endTime time.Time) (*Event, error) {
	metadata, err := h.repository.FindMetadataByGuildID(guildID)
	if err != nil {
		return nil, err
	}
	channel, err := h.findChannelByName(s, guildID, metadata.ChannelName)
	if err != nil {
		return nil, err
	}

	msg, err := s.ChannelMessageSend(channel.ID, buildEventContent(name, startTime, endTime, description, location, ""))
	if err != nil {
		return nil, err
	}

	if err := s.MessageReactionAdd(msg.ChannelID, msg.ID, REACTION_YES); err != nil {
		return nil, err
	}
	if err := s.MessageReactionAdd(msg.ChannelID, msg.ID, REACTION_NO); err != nil {
		return nil, err
	}

	event := Event{
		GuildID:     guildID,
		MessageID:   msg.ID,
		Name:        name,
		Description: description,
		Location:    location,
		StartTime:   startTime,
		EndTime:     endTime,
		Answers:     []Answer{},
	}

	event, err = h.repository.CreateEvent(event)
	if err != nil {
		return nil, err
	}

	return &event, nil
}

func (h *Handler) check(s *discordgo.Session) {
	fmt.Println("Running check")
	now := time.Now()

	for _, guild := range s.State.Guilds {
		// Check fixed training times
		for _, trainingTime := range h.config.FixedTrainingTimes {
			if int(now.Weekday()) == trainingTime.WeekDay {
				t1 := time.Date(now.Year(), now.Month(), now.Day(), trainingTime.StartTimeHours, trainingTime.StartTimeMinutes, 0, now.Nanosecond(), now.Location())
				t2 := time.Date(now.Year(), now.Month(), now.Day(), trainingTime.EndTimeHours, trainingTime.EndTimeMinutes, 0, now.Nanosecond(), now.Location())

				if t1.Before(now) {
					continue
				}

				// Don't create event if its before 9 AM, except if time till event is less than 2 hours
				diff := t1.Sub(now)
				if now.Hour() < 9 && diff.Hours() > 2 {
					continue
				}

				events, err := h.repository.FindEvents(guild.ID)

				_, ok := lo.Find(events, func(event Event) bool {
					return event.Name == trainingTime.Name &&
						int(event.StartTime.Weekday()) == trainingTime.WeekDay &&
						event.StartTime.Hour() == trainingTime.StartTimeHours &&
						event.StartTime.Minute() == trainingTime.StartTimeMinutes
				})

				if ok {
					continue
				}

				_, err = h.createNewEvent(s, guild.ID, trainingTime.Name, trainingTime.Description, trainingTime.Location, t1, t2)
				if err != nil {
					fmt.Printf("Error creating an event %v\n", err)
					continue
				}
			}
		}
	}
}
