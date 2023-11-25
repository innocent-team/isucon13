package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/jmoiron/sqlx"
)

type ImageModel struct {
	ID     int64  `db:"id"`
	UserId int64  `db:"user_id"`
	Image  []byte `db:"image"`
}

func bulkFillUserReponse(ctx context.Context, db sqlx.QueryerContext, userModels []UserModel) (map[int64]User, error) {
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
	fallbackImageData, err := os.ReadFile(fallbackImage)
	if err != nil {
		return nil, fmt.Errorf("failed to read fallbackImage: %w", err)
	}
	imageByUserId := make(map[int64][]byte)
	for _, userId := range userIds {
		imageByUserId[userId] = fallbackImageData
	}
	{
		query, args, err := sqlx.In("SELECT * FROM icons WHERE user_id IN (?)", userIds)
		if err != nil {
			return nil, fmt.Errorf("failed to construct IN query for icons: %w", err)
		}

		imageModels := []ImageModel{}
		if err := sqlx.SelectContext(ctx, db, &imageModels, query, args...); err != nil {
			return nil, fmt.Errorf("failed to query icons: %w", err)
		}
		for _, imageModel := range imageModels {
			imageByUserId[imageModel.UserId] = imageModel.Image
		}
	}

	// User.ID -> User にして返す
	userById := make(map[int64]User)
	for _, userModel := range userModels {
		themeModel := themeByUserId[userModel.ID]
		image := imageByUserId[userModel.ID]
		iconHash := sha256.Sum256(image)

		user := User{
			ID:          userModel.ID,
			Name:        userModel.Name,
			DisplayName: userModel.DisplayName,
			Description: userModel.Description,
			Theme: Theme{
				ID:       themeModel.ID,
				DarkMode: themeModel.DarkMode,
			},
			IconHash: fmt.Sprintf("%x", iconHash),
		}
		userById[user.ID] = user
	}

	return userById, nil
}
