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
	Answers  []Answer
}

type Answer struct {
	gorm.Model
	YesNo   bool
	EventID uint `gorm:"uniqueIndex:event_user_unique_index"`
	UserID  uint `gorm:"uniqueIndex:event_user_unique_index"`
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
