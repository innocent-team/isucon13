package main

import (
	"context"
	"fmt"

	"github.com/hatena/godash"
	"github.com/jmoiron/sqlx"
)

func bulkFillLivecommentResponse(ctx context.Context, db sqlx.QueryerContext, commentModels []LivecommentModel) ([]Livecomment, error) {
	if len(commentModels) == 0 {
		return []Livecomment{}, nil
	}

	var commentOwners []UserModel
	{
		commentUserIds := godash.Map(commentModels, func(c LivecommentModel, _ int) int64 { return c.UserID })
		query, args, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", commentUserIds)
		if err != nil {
			return nil, fmt.Errorf("failed to construct IN query: %w", err)
		}
		if err := sqlx.SelectContext(ctx, db, &commentOwners, query, args...); err != nil {
			return nil, fmt.Errorf("failed to query users: %w", err)
		}
	}
	userById, err := bulkFillUserResponse(ctx, db, commentOwners)
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

	// themesをbulk getする
	themeByUserId := make(map[int64]ThemeModel)
	{
		query, args, err := sqlx.In("SELECT * FROM themes WHERE user_id IN (?)", userIds)
		if err != nil {
			return nil, fmt.Errorf("failed to construct IN query for themes: %w", err)
		}

		themeModels := []ThemeModel{}
		if err := sqlx.SelectContext(ctx, db, &themeModels, query, args...); err != nil {
			return nil, fmt.Errorf("failed to query themes: %w", err)
		}
		for _, themeModel := range themeModels {
			themeByUserId[themeModel.UserID] = themeModel
		}
	}

	// imagesをbulk getする
	hashByUserId := make(map[int64]string)
	for _, userId := range userIds {
		hashByUserId[userId] = fallbackImageHash
	}
	{
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
	}

	// User.ID -> User にして返す
	userById := make(map[int64]User)
	for _, userModel := range userModels {
		themeModel := themeByUserId[userModel.ID]
		iconHash := hashByUserId[userModel.ID]

		user := User{
			ID:          userModel.ID,
			Name:        userModel.Name,
			DisplayName: userModel.DisplayName,
			Description: userModel.Description,
			Theme: Theme{
				ID:       themeModel.ID,
				DarkMode: themeModel.DarkMode,
			},
			IconHash: iconHash,
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

	var livestreamTagModels []*LivestreamTagModel
	query, args, err := sqlx.In("SELECT * FROM livestream_tags WHERE livestream_id IN (?)", livestreamIds)
	if err != nil {
		return nil, fmt.Errorf("failed to construct IN query for livestream_tags: %w", err)
	}
	if err := sqlx.SelectContext(ctx, db, &livestreamTagModels, query, args...); err != nil {
		return nil, fmt.Errorf("failed to construct query livestream_tags: %w", err)
	}
	tagIds := make([]int64, len(livestreamTagModels))
	for i, livestreamTagModel := range livestreamTagModels {
		tagIds[i] = livestreamTagModel.TagID
	}

	tagsByLivestreamId := make(map[int64][]Tag)
	// nilにならないように空スライスを埋めておく
	for _, livestreamModel := range livestreamModels {
		tagsByLivestreamId[livestreamModel.ID] = make([]Tag, 0)
	}
	for _, livestreamTagModel := range livestreamTagModels {
		tag := *tagsAll[livestreamTagModel.TagID]
		tagsByLivestreamId[livestreamTagModel.LivestreamID] = append(tagsByLivestreamId[livestreamTagModel.LivestreamID], tag)
	}

	return tagsByLivestreamId, nil
}
