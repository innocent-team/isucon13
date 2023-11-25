package main

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

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
	hashByUserId, err := getIconHashByIds(ctx, db, userIds)
	if err != nil {
		return nil, fmt.Errorf("failed to get icon hash by ids: %w", err)
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
