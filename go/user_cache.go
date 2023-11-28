package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type UserCacheData struct {
	user      UserModel
	expiresAt time.Time
}

var userCacheMap = make(map[int64]UserCacheData)
var userCacheMu = sync.RWMutex{}

func fetchUsers(ctx context.Context, tx sqlx.QueryerContext, userIds []int64) (map[int64]UserModel, error) {
	if len(userIds) == 0 {
		return make(map[int64]UserModel), nil
	}

	userResp := make(map[int64]UserModel)
	notFoundUserIds := func() []int64 {
		userCacheMu.RLock()
		defer userCacheMu.RUnlock()

		var notFoundUserIds []int64
		for _, userId := range userIds {
			userData, ok := userCacheMap[userId]
			if !ok {
				notFoundUserIds = append(notFoundUserIds, userId)
				continue
			}
			if userData.expiresAt.After(time.Now()) {
				notFoundUserIds = append(notFoundUserIds, userId)
				continue
			}
			userResp[userId] = userData.user
		}
		return notFoundUserIds
	}()

	if len(notFoundUserIds) == 0 {
		return userResp, nil
	}

	userModels := []UserModel{}
	{
		query, args, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", notFoundUserIds)
		if err != nil {
			return nil, fmt.Errorf("IN query: %w", err)
		}
		if err := sqlx.SelectContext(ctx, tx, &userModels, query, args...); err != nil {
			return nil, fmt.Errorf("SELECT users: %w", err)
		}
	}

	userCacheMu.Lock()
	for _, user := range userModels {
		userResp[user.ID] = user
		userCacheMap[user.ID] = UserCacheData{
			user:      user,
			expiresAt: time.Now().Add(1 * time.Second),
		}
	}
	userCacheMu.Unlock()

	return userResp, nil
}

func fetchUser(ctx context.Context, tx sqlx.QueryerContext, userId int64) (UserModel, error) {
	userById, err := fetchUsers(ctx, tx, []int64{userId})
	if err != nil {
		return UserModel{}, err
	}
	user, ok := userById[userId]
	if !ok {
		return UserModel{}, fmt.Errorf("user %d not found", userId)
	}
	return user, nil
}
