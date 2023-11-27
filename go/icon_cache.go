package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"strconv"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/hatena/godash"
	"github.com/jmoiron/sqlx"
)

type IconCacheData struct {
	hash   string
	userID int64
}

func memcachedIconCacheKey(userId int64) string {
	return "icon:" + strconv.FormatInt(userId, 10)
}

func getIconHashByIds(ctx context.Context, db sqlx.QueryerContext, userIds []int64) (map[int64]string, error) {
	items, err := memd.GetMulti(godash.Map(userIds, func(uid int64, _ int) string { return memcachedIconCacheKey(uid) }))
	if err != nil {
		return nil, err
	}

	resHashByUserId := make(map[int64]string)
	needFetchUserIds := []int64{}
	for _, userId := range userIds {
		data, ok := items[memcachedIconCacheKey(userId)]
		if !ok {
			needFetchUserIds = append(needFetchUserIds, userId)
			continue
		}
		iconData := IconCacheData{}
		if err := gob.NewDecoder(bytes.NewBuffer(data.Value)).Decode(&iconData); err != nil {
			return nil, err
		}
		resHashByUserId[userId] = iconData.hash
	}

	if len(needFetchUserIds) > 0 {
		hashByUserId, err := fetchIconByIds(ctx, db, needFetchUserIds)
		if err != nil {
			return nil, err
		}

		for _, userId := range needFetchUserIds {
			var encoded bytes.Buffer
			if err := gob.NewEncoder(&encoded).Encode(IconCacheData{
				hash:   hashByUserId[userId],
				userID: userId,
			}); err != nil {
				return nil, err
			}
			memd.Set(&memcache.Item{
				Key:   memcachedIconCacheKey(userId),
				Value: encoded.Bytes(),
			})
			resHashByUserId[userId] = hashByUserId[userId]
		}
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
