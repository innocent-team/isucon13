package main

import (
	"context"
	"fmt"

	"github.com/hatena/godash"
	"github.com/jmoiron/sqlx"
	"golang.org/x/exp/maps"
)

func bulkFillLivecommentResponse(ctx context.Context, db sqlx.QueryerContext, commentModels []LivecommentModel) ([]Livecomment, error) {
	if len(commentModels) == 0 {
		return []Livecomment{}, nil
	}

	commentUserIds := godash.Map(commentModels, func(c LivecommentModel, _ int) int64 { return c.UserID })
	commentOwners, err := fetchUsers(ctx, db, commentUserIds)
	if err != nil {
		return nil, fmt.Errorf("failed to fetchUsers: %w", err)
	}
	userById, err := bulkFillUserResponse(ctx, db, maps.Values(commentOwners))
	if err != nil {
		return nil, fmt.Errorf("failed to bulkFillUserResponse: %w", err)
	}
	var livestreamModels []*LivestreamModel
	{
		commentLivestreamIds := godash.Map(commentModels, func(c LivecommentModel, _ int) int64 { return c.LivestreamID })
		query, args, err := sqlx.In("SELECT * FROM livestreams WHERE id IN (?)", commentLivestreamIds)
		if err != nil {
			return nil, fmt.Errorf("failed to construct IN query: %w", err)
		}
		if err := sqlx.SelectContext(ctx, db, &livestreamModels, query, args...); err != nil {
			return nil, fmt.Errorf("failed to query users: %w", err)
		}
	}

	liveStreams, err := bulkFillLivestreamResponse(ctx, db, livestreamModels)
	if err != nil {
		return nil, fmt.Errorf("failed to bulkFillLivestreamResponse: %w", err)
	}
	livestreamById := make(map[int64]Livestream)
	for _, livestream := range liveStreams {
		livestreamById[livestream.ID] = livestream
	}

	livecomments := make([]Livecomment, len(commentModels))
	for i, livecommentModel := range commentModels {
		commentOwner := userById[livecommentModel.UserID]
		livecomment := Livecomment{
			ID:         livecommentModel.ID,
			User:       commentOwner,
			Livestream: livestreamById[livecommentModel.LivestreamID],
			Comment:    livecommentModel.Comment,
			Tip:        livecommentModel.Tip,
			CreatedAt:  livecommentModel.CreatedAt,
		}
		livecomments[i] = livecomment
	}

	return livecomments, nil
}

type ImageModel struct {
	ID     int64  `db:"id"`
	UserId int64  `db:"user_id"`
	Image  []byte `db:"image"`
	Hash   string `db:"hash"`
}

func bulkFillUserResponse(ctx context.Context, db sqlx.QueryerContext, userModels []UserModel) (map[int64]User, error) {
	if len(userModels) == 0 {
		return make(map[int64]User), nil
	}

	userIds := make([]int64, len(userModels))
	for i, userModel := range userModels {
		userIds[i] = userModel.ID
	}

	// User.ID -> User にして返す
	userById := make(map[int64]User)
	for _, userModel := range userModels {
		user := User{
			ID:          userModel.ID,
			Name:        userModel.Name,
			DisplayName: userModel.DisplayName,
			Description: userModel.Description,
			Theme: Theme{
				ID:       userModel.ID,
				DarkMode: userModel.DarkMode,
			},
			IconHash: userModel.GetIconHash(),
		}
		userById[user.ID] = user
	}

	return userById, nil
}

// Livestream.ID -> []Tag
func bulkGetTagsByLivestream(ctx context.Context, db sqlx.QueryerContext, livestreamModels []*LivestreamModel) (map[int64][]Tag, error) {
	if len(livestreamModels) == 0 {
		return nil, nil
	}

	livestreamIds := make([]int64, len(livestreamModels))
	for i, livestreamModel := range livestreamModels {
		livestreamIds[i] = livestreamModel.ID
	}

	tagsByLivestreamId := make(map[int64][]Tag)
	for _, livestreamModel := range livestreamModels {
		// nilにならないように空スライスを埋めておく
		tagsByLivestreamId[livestreamModel.ID] = make([]Tag, 0)
		if len(livestreamModel.TagIds) > 0 {
			tagsByLivestreamId[livestreamModel.ID] = godash.Map([]int64(livestreamModel.TagIds), func(tagId int64, _ int) Tag {
				return *tagsAll[tagId]
			})
		}
	}

	return tagsByLivestreamId, nil
}
