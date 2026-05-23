package models

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type FaviconCache struct {
	BaseModel
	SourceURL    string    `json:"sourceUrl" gorm:"size:1024;index"`
	FinalIconURL string    `json:"finalIconUrl" gorm:"size:1024;uniqueIndex"`
	LocalSrc     string    `json:"localSrc" gorm:"size:1024"`
	FileId       uint      `json:"fileId" gorm:"index"`
	ContentHash  string    `json:"contentHash" gorm:"size:128;index"`
	ContentType  string    `json:"contentType" gorm:"size:100"`
	Size         int64     `json:"size"`
	LastUsedAt   time.Time `json:"lastUsedAt" gorm:"index"`
}

func (m *FaviconCache) FindByFinalIconURL(db *gorm.DB, finalIconURL string) (FaviconCache, error) {
	cache := FaviconCache{}
	err := db.First(&cache, "final_icon_url = ?", finalIconURL).Error
	return cache, err
}

func (m *FaviconCache) FindByContentHash(db *gorm.DB, contentHash string, size int64) (FaviconCache, error) {
	cache := FaviconCache{}
	err := db.First(&cache, "content_hash = ? AND size = ?", contentHash, size).Error
	return cache, err
}

func (m *FaviconCache) Touch(db *gorm.DB) error {
	now := time.Now()
	if err := db.Model(m).Update("last_used_at", now).Error; err != nil {
		return err
	}
	m.LastUsedAt = now
	return nil
}

func (m *FaviconCache) CleanupUnused(db *gorm.DB, before time.Time, removeFile func(string) error) (int64, error) {
	caches := []FaviconCache{}
	if err := db.Where("last_used_at < ?", before).Find(&caches).Error; err != nil {
		return 0, err
	}

	var removed int64
	for _, cache := range caches {
		var refs int64
		localSrc := strings.TrimSpace(cache.LocalSrc)
		if localSrc != "" {
			trimmedSrc := strings.TrimPrefix(localSrc, ".")
			if err := db.Model(&ItemIcon{}).
				Where("icon_json LIKE ? OR icon_json LIKE ?", "%"+localSrc+"%", "%"+trimmedSrc+"%").
				Count(&refs).Error; err != nil {
				return removed, err
			}
		}
		if refs > 0 {
			continue
		}

		if removeFile != nil && localSrc != "" {
			if err := removeFile(localSrc); err != nil {
				return removed, err
			}
		}
		if err := db.Delete(&FaviconCache{}, cache.ID).Error; err != nil {
			return removed, err
		}
		removed++
	}

	return removed, nil
}
