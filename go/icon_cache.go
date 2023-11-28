package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

type IconCacheData struct {
	hash   string
	userID int64
}

var iconCache = make(map[int64]IconCacheData)
var iconCacheMutex = sync.RWMutex{}

func getIconHashByIds(ctx context.Context, db sqlx.QueryerContext, userIds []int64) (map[int64]string, error) {
	resHashByUserId := make(map[int64]string)
	iconCacheMutex.RLock()
	needFetchUserIds := []int64{}
	for _, userId := range userIds {
		data, ok := iconCache[userId]
		if ok {
			resHashByUserId[userId] = data.hash
			continue
		}
		needFetchUserIds = append(needFetchUserIds, userId)
	}
	iconCacheMutex.RUnlock()

	if len(needFetchUserIds) > 0 {
		hashByUserId, err := fetchIconByIds(ctx, db, needFetchUserIds)
		if err != nil {
			return nil, err
		}
		iconCacheMutex.Lock()
		for _, userId := range needFetchUserIds {
			iconCache[userId] = IconCacheData{
				hash:   hashByUserId[userId],
				userID: userId,
			}
			resHashByUserId[userId] = hashByUserId[userId]
		}
		iconCacheMutex.Unlock()
	}

	return resHashByUserId, nil
}

func getIconHashById(ctx context.Context, db sqlx.QueryerContext, userId int64) (string, error) {
	hashByUserId, err := getIconHashByIds(ctx, db, []int64{userId})
	if err != nil {
		return "", err
	}
	return hashByUserId[userId], nil
}

func updateIconHash(ctx context.Context, userId int64, newHash string) error {
	data := IconCacheData{
		hash:   newHash,
		userID: userId,
	}

	iconCacheMutex.Lock()
	defer iconCacheMutex.Unlock()
	iconCache[userId] = data

	return nil
}

func purgeIconHash(ctx context.Context) error {
	iconCacheMutex.Lock()
	defer iconCacheMutex.Unlock()

	iconCache = make(map[int64]IconCacheData)

	return nil
}

func fetchIconByIds(ctx context.Context, db sqlx.QueryerContext, userIds []int64) (map[int64]string, error) {
	hashByUserId := make(map[int64]string)
	for _, userId := range userIds {
		hashByUserId[userId] = fallbackImageHash
	}
	query, args, err := sqlx.In("SELECT user_id, hash FROM icons WHERE user_id IN (?)", userIds)
	if err != nil {
		return nil, fmt.Errorf("failed to construct IN query for icons: %w", err)
	}

	imageModels := []ImageModel{}
	if err := sqlx.SelectContext(ctx, db, &imageModels, query, args...); err != nil {
		return nil, fmt.Errorf("failed to query icons: %w", err)
	}
	for _, imageModel := range imageModels {
		hashByUserId[imageModel.UserId] = imageModel.Hash
	}
	return hashByUserId, nil
}
