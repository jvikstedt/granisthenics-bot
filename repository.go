package main

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrRecordNotFound = errors.New("record not found")

type FixedTrainingTime struct {
	gorm.Model
	GuildID          string
	WeekDay          int
	StartTimeHours   int
	StartTimeMinutes int
	EndTimeHours     int
	EndTimeMinutes   int
	Name             string
	Description      string
	Location         string
}

type User struct {
	gorm.Model
	GuildID  string `gorm:"uniqueIndex:guild_member_unique_index"`
	MemberID string `gorm:"uniqueIndex:guild_member_unique_index"`
	Name     string
}

type Answer struct {
	gorm.Model
	YesNo   string
	EventID uint `gorm:"uniqueIndex:event_user_unique_index"`
	UserID  uint `gorm:"uniqueIndex:event_user_unique_index"`
	User    User
}

type Event struct {
	gorm.Model
	GuildID     string `gorm:"uniqueIndex:guild_message_unique_index"`
	MessageID   string `gorm:"uniqueIndex:guild_message_unique_index"`
	Name        string
	Description string
	Location    string
	StartTime   time.Time
	EndTime     time.Time
	Answers     []Answer
}

type Metadata struct {
	gorm.Model
	GuildID       string `gorm:"unique"`
	LastWeekReset time.Time
	ChannelName   string
}

type Repository struct {
	db *gorm.DB
}

func (r *Repository) FindMetadataByGuildID(guildID string) (Metadata, error) {
	metadata := Metadata{}
	result := r.db.Where("guild_id = ?", guildID).First(&metadata)
	err := result.Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return metadata, ErrRecordNotFound
	}

	return metadata, err
}

func (r *Repository) CreateMetadata(metadata Metadata) (Metadata, error) {
	result := r.db.Create(&metadata)

	return metadata, result.Error
}

func (r *Repository) FindEvent(guildID string, messageID string) (Event, error) {
	event := Event{}
	result := r.db.Where("guild_id = ? AND message_id = ?", guildID, messageID).Preload("Answers").First(&event)
	err := result.Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return event, ErrRecordNotFound
	}

	return event, err
}

func (r *Repository) FindEvents(guildID string) ([]Event, error) {
	events := []Event{}
	result := r.db.Where("guild_id = ?", guildID).Find(&events)

	return events, result.Error
}

func (r *Repository) CreateEvent(event Event) (Event, error) {
	result := r.db.Create(&event)

	return event, result.Error
}

func (r *Repository) CreateUser(user User) (User, error) {
	result := r.db.Create(&user)

	return user, result.Error
}

func (r *Repository) FindUser(guildID string, memberID string) (User, error) {
	user := User{}
	result := r.db.Where("guild_id = ? AND member_id = ?", guildID, memberID).First(&user)
	err := result.Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return user, ErrRecordNotFound
	}

	return user, err
}

func (r *Repository) FindUsers(guildID string) ([]User, error) {
	users := []User{}
	result := r.db.Where("guild_id = ?", guildID).Find(&users)

	return users, result.Error
}

func (r *Repository) FindAnswer(eventID uint, userID uint) (Answer, error) {
	answer := Answer{}
	result := r.db.Where("event_id = ? AND user_id = ?", eventID, userID).First(&answer)
	err := result.Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return answer, ErrRecordNotFound
	}

	return answer, result.Error
}

func (r *Repository) UpdateOrCreateAnswer(answer Answer) (Answer, error) {
	result := r.db.Model(&answer).Where("id = ?", answer.ID).Updates(&answer)

	if result.RowsAffected == 0 {
		result = r.db.Create(&answer)
	}

	return answer, result.Error
}
