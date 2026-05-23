package models

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newFaviconCacheTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite memory db: %v", err)
	}
	if err := db.AutoMigrate(&FaviconCache{}, &ItemIcon{}); err != nil {
		t.Fatalf("auto migrate favicon cache: %v", err)
	}
	return db
}

func TestFaviconCacheFindByFinalIconURL(t *testing.T) {
	db := newFaviconCacheTestDB(t)
	cache := FaviconCache{
		SourceURL:    "http://nas.local",
		FinalIconURL: "http://nas.local/favicon.ico",
		LocalSrc:     "./uploads/favicon.ico",
		ContentHash:  "hash-a",
		ContentType:  "image/x-icon",
		Size:         128,
		LastUsedAt:   time.Now().Add(-time.Hour),
	}
	if err := db.Create(&cache).Error; err != nil {
		t.Fatalf("create cache: %v", err)
	}

	got, err := (&FaviconCache{}).FindByFinalIconURL(db, "http://nas.local/favicon.ico")
	if err != nil {
		t.Fatalf("find cache by final icon URL: %v", err)
	}
	if got.ID != cache.ID || got.LocalSrc != cache.LocalSrc {
		t.Fatalf("unexpected cache result: %#v", got)
	}
}

func TestFaviconCacheFindByContentHash(t *testing.T) {
	db := newFaviconCacheTestDB(t)
	cache := FaviconCache{
		SourceURL:    "http://router.lan",
		FinalIconURL: "http://router.lan/favicon.png",
		LocalSrc:     "./uploads/shared.png",
		ContentHash:  "same-content",
		ContentType:  "image/png",
		Size:         256,
		LastUsedAt:   time.Now(),
	}
	if err := db.Create(&cache).Error; err != nil {
		t.Fatalf("create cache: %v", err)
	}

	got, err := (&FaviconCache{}).FindByContentHash(db, "same-content", 256)
	if err != nil {
		t.Fatalf("find cache by content hash: %v", err)
	}
	if got.ID != cache.ID || got.FinalIconURL != cache.FinalIconURL {
		t.Fatalf("unexpected cache result: %#v", got)
	}
}

func TestFaviconCacheTouchUpdatesLastUsedAt(t *testing.T) {
	db := newFaviconCacheTestDB(t)
	oldTime := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	cache := FaviconCache{
		SourceURL:    "http://app.lan",
		FinalIconURL: "http://app.lan/favicon.ico",
		LocalSrc:     "./uploads/app.ico",
		ContentHash:  "hash-touch",
		Size:         64,
		LastUsedAt:   oldTime,
	}
	if err := db.Create(&cache).Error; err != nil {
		t.Fatalf("create cache: %v", err)
	}

	if err := cache.Touch(db); err != nil {
		t.Fatalf("touch cache: %v", err)
	}

	var got FaviconCache
	if err := db.First(&got, cache.ID).Error; err != nil {
		t.Fatalf("reload cache: %v", err)
	}
	if !got.LastUsedAt.After(oldTime) {
		t.Fatalf("expected last_used_at to be refreshed, got %s old %s", got.LastUsedAt, oldTime)
	}
}

func TestFaviconCacheCleanupUnusedRemovesOnlyUnreferencedOldCaches(t *testing.T) {
	db := newFaviconCacheTestDB(t)
	cutoff := time.Now().Add(-24 * time.Hour)
	oldUnreferenced := FaviconCache{
		SourceURL:    "http://old.lan",
		FinalIconURL: "http://old.lan/favicon.ico",
		LocalSrc:     "./uploads/old.ico",
		ContentHash:  "old",
		Size:         64,
		LastUsedAt:   cutoff.Add(-time.Hour),
	}
	oldReferenced := FaviconCache{
		SourceURL:    "http://keep.lan",
		FinalIconURL: "http://keep.lan/favicon.ico",
		LocalSrc:     "./uploads/keep.ico",
		ContentHash:  "keep",
		Size:         64,
		LastUsedAt:   cutoff.Add(-time.Hour),
	}
	recent := FaviconCache{
		SourceURL:    "http://recent.lan",
		FinalIconURL: "http://recent.lan/favicon.ico",
		LocalSrc:     "./uploads/recent.ico",
		ContentHash:  "recent",
		Size:         64,
		LastUsedAt:   cutoff.Add(time.Hour),
	}
	if err := db.Create(&[]FaviconCache{oldUnreferenced, oldReferenced, recent}).Error; err != nil {
		t.Fatalf("create caches: %v", err)
	}
	if err := db.Create(&ItemIcon{IconJson: `{"itemType":2,"src":"./uploads/keep.ico"}`}).Error; err != nil {
		t.Fatalf("create referenced item icon: %v", err)
	}

	removedFiles := make([]string, 0)
	removed, err := (&FaviconCache{}).CleanupUnused(db, cutoff, func(filePath string) error {
		removedFiles = append(removedFiles, filePath)
		return nil
	})
	if err != nil {
		t.Fatalf("cleanup unused caches: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected one cache removed, got %d", removed)
	}
	if len(removedFiles) != 1 || removedFiles[0] != "./uploads/old.ico" {
		t.Fatalf("unexpected removed files: %v", removedFiles)
	}

	var count int64
	if err := db.Model(&FaviconCache{}).Where("local_src = ?", "./uploads/old.ico").Count(&count).Error; err != nil {
		t.Fatalf("count old cache: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected old unreferenced cache to be deleted")
	}
	if err := db.Model(&FaviconCache{}).Where("local_src IN ?", []string{"./uploads/keep.ico", "./uploads/recent.ico"}).Count(&count).Error; err != nil {
		t.Fatalf("count retained caches: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected referenced and recent caches to remain, got %d", count)
	}
}
