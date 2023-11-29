package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/goccy/go-json"
	"golang.org/x/exp/maps"

	"github.com/hatena/godash"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

type ReactionModel struct {
	ID           int64  `db:"id"`
	EmojiName    string `db:"emoji_name"`
	UserID       int64  `db:"user_id"`
	LivestreamID int64  `db:"livestream_id"`
	CreatedAt    int64  `db:"created_at"`
}

type Reaction struct {
	ID         int64      `json:"id"`
	EmojiName  string     `json:"emoji_name"`
	User       User       `json:"user"`
	Livestream Livestream `json:"livestream"`
	CreatedAt  int64      `json:"created_at"`
}

type PostReactionRequest struct {
	EmojiName string `json:"emoji_name"`
}

func getReactionsHandler(c echo.Context) error {
	ctx := c.Request().Context()

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	livestreamID, err := strconv.Atoi(c.Param("livestream_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "livestream_id in path must be integer")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	query := "SELECT * FROM reactions WHERE livestream_id = ? ORDER BY created_at DESC"
	if c.QueryParam("limit") != "" {
		limit, err := strconv.Atoi(c.QueryParam("limit"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "limit query parameter must be integer")
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	reactionModels := []ReactionModel{}
	if err := tx.SelectContext(ctx, &reactionModels, query, livestreamID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "failed to get reactions")
	}

	reactions, err := bulkFillReactionResponse(ctx, tx, reactionModels)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to bulkFillReactionResponse: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, reactions)
}

func bulkFillReactionResponse(ctx context.Context, tx *sqlx.Tx, reactionModels []ReactionModel) ([]Reaction, error) {
	if len(reactionModels) == 0 {
		return []Reaction{}, nil
	}

	userIds := godash.Map(reactionModels, func(r ReactionModel, _ int) int64 { return r.UserID })
	userModels, err := fetchUsers(ctx, tx, userIds)
	if err != nil {
		return nil, fmt.Errorf("failed to fetchUsers: %w", err)
	}
	userById, err := bulkFillUserResponse(ctx, maps.Values(userModels))
	if err != nil {
		return nil, fmt.Errorf("failed to bulkFillUserResponse: %w", err)
	}
	livestreamIds := godash.Map(reactionModels, func(r ReactionModel, _ int) int64 { return r.LivestreamID })
	livestreamModels, err := fetchLivestreams(ctx, tx, livestreamIds)
	if err != nil {
		return nil, fmt.Errorf("failed to fetchLivestreams: %w", err)
	}
	livestreams, err := bulkFillLivestreamResponse(ctx, tx, maps.Values(livestreamModels))
	if err != nil {
		return nil, fmt.Errorf("failed to bulkFillLivestreamResponse: %w", err)
	}
	livestreamById := godash.KeyBy(livestreams, func(l Livestream) int64 { return l.ID })

	reactions := make([]Reaction, len(reactionModels))
	for i, reactionModel := range reactionModels {
		user := userById[reactionModel.UserID]
		livestream := livestreamById[reactionModel.LivestreamID]

		reaction := Reaction{
			ID:         reactionModel.ID,
			EmojiName:  reactionModel.EmojiName,
			User:       user,
			Livestream: livestream,
			CreatedAt:  reactionModel.CreatedAt,
		}

		reactions[i] = reaction
	}
	return reactions, nil
}

func postReactionHandler(c echo.Context) error {
	ctx := c.Request().Context()
	livestreamID, err := strconv.Atoi(c.Param("livestream_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "livestream_id in path must be integer")
	}

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	var req *PostReactionRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	reactionModel := ReactionModel{
		UserID:       int64(userID),
		LivestreamID: int64(livestreamID),
		EmojiName:    req.EmojiName,
		CreatedAt:    time.Now().Unix(),
	}

	result, err := dbConn.NamedExecContext(ctx, "INSERT INTO reactions (user_id, livestream_id, emoji_name, created_at) VALUES (:user_id, :livestream_id, :emoji_name, :created_at)", reactionModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert reaction: "+err.Error())
	}

	reactionID, err := result.LastInsertId()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get last inserted reaction id: "+err.Error())
	}
	reactionModel.ID = reactionID

	reaction, err := fillReactionResponse(ctx, dbConn, reactionModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill reaction: "+err.Error())
	}

	return c.JSON(http.StatusCreated, reaction)
}

func fillReactionResponse(ctx context.Context, tx sqlx.QueryerContext, reactionModel ReactionModel) (Reaction, error) {
	userModel, err := fetchUser(ctx, tx, reactionModel.UserID)
	if err != nil {
		return Reaction{}, err
	}
	user, err := fillUserResponse(ctx, userModel)
	if err != nil {
		return Reaction{}, err
	}

	livestreamModel, err := fetchLivestream(ctx, tx, reactionModel.LivestreamID)
	if err != nil {
		return Reaction{}, err
	}
	livestream, err := fillLivestreamResponse(ctx, tx, *livestreamModel)
	if err != nil {
		return Reaction{}, err
	}

	reaction := Reaction{
		ID:         reactionModel.ID,
		EmojiName:  reactionModel.EmojiName,
		User:       user,
		Livestream: livestream,
		CreatedAt:  reactionModel.CreatedAt,
	}

	return reaction, nil
}
