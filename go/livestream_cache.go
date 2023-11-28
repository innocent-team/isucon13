package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

const LivestreamCacheTTL = 30 * time.Second

var ErrLivestreamNotFound = errors.New("livestream not found")

type LivestreamCacheData struct {
	livestream *LivestreamModel
	expiresAt  time.Time
}

var livestreamCacheMap = make(map[int64]LivestreamCacheData)
var livestreamCacheMu = sync.RWMutex{}

func fetchLivestreams(ctx context.Context, tx sqlx.QueryerContext, ids []int64) (map[int64]*LivestreamModel, error) {
	if len(ids) == 0 {
		return make(map[int64]*LivestreamModel), nil
	}

	livestreamResp := make(map[int64]*LivestreamModel)
	notFoundIds := func() []int64 {
		livestreamCacheMu.RLock()
		defer livestreamCacheMu.RUnlock()

		var notFoundIds []int64
		for _, livestreamId := range ids {
			data, ok := livestreamCacheMap[livestreamId]
			if !ok {
				notFoundIds = append(notFoundIds, livestreamId)
				continue
			}
			if time.Now().After(data.expiresAt) {
				notFoundIds = append(notFoundIds, livestreamId)
				continue
			}
			livestreamResp[livestreamId] = data.livestream
		}
		return notFoundIds
	}()

	if len(notFoundIds) == 0 {
		return livestreamResp, nil
	}

	livestreamModels := []*LivestreamModel{}
	{
		query, args, err := sqlx.In("SELECT * FROM livestreams WHERE id IN (?)", notFoundIds)
		if err != nil {
			return nil, fmt.Errorf("IN query: %w", err)
		}
		if err := sqlx.SelectContext(ctx, tx, &livestreamModels, query, args...); err != nil {
			return nil, fmt.Errorf("SELECT livestreams: %w", err)
		}
	}

	livestreamCacheMu.Lock()
	for _, livestream := range livestreamModels {
		livestreamResp[livestream.ID] = livestream
		livestreamCacheMap[livestream.ID] = LivestreamCacheData{
			livestream: livestream,
			expiresAt:  time.Now().Add(LivestreamCacheTTL),
		}
	}
	livestreamCacheMu.Unlock()

	return livestreamResp, nil
}

func fetchLivestream(ctx context.Context, tx sqlx.QueryerContext, id int64) (*LivestreamModel, error) {
	userById, err := fetchLivestreams(ctx, tx, []int64{id})
	if err != nil {
		return nil, err
	}
	user, ok := userById[id]
	if !ok {
		return nil, ErrLivestreamNotFound
	}
	return user, nil
}
