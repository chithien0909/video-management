package entity

import (
	"gorm.io/gorm"
)

type Video struct {
	gorm.Model
	Filename string
	Path     string
	HLSPath  string
	FileSize int64
}

func (Video) TableName() string {
	return "videos"
}
