package main

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Tag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type TagModel struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

type TagsResponse struct {
	Tags []*Tag `json:"tags"`
}

var tagsResponseCache *TagsResponse

func getTagHandler(c echo.Context) error {
	if tagsResponseCache != nil {
		return c.JSON(http.StatusOK, tagsResponseCache)
	}

	tags := make([]*Tag, len(tagsAll))
	for id, tag := range tagsAll {
		tags[id-1] = tag
	}
	tagsResponseCache = &TagsResponse{
		Tags: tags,
	}
	return c.JSON(http.StatusOK, tagsResponseCache)
}

// 配信者のテーマ取得API
// GET /api/user/:username/theme
func getStreamerThemeHandler(c echo.Context) error {
	ctx := c.Request().Context()

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		c.Logger().Printf("verifyUserSession: %+v\n", err)
		return err
	}

	username := c.Param("username")

	userModel := UserModel{}
	err := dbConn.GetContext(ctx, &userModel, "SELECT id FROM users WHERE name = ?", username)
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "not found user that has the given username")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}

	theme := Theme{
		ID:       userModel.ID,
		DarkMode: userModel.DarkMode,
	}

	return c.JSON(http.StatusOK, theme)
}
