package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/goccy/go-json"

	"github.com/hatena/godash"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

type ReserveLivestreamRequest struct {
	Tags         []int64 `json:"tags"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	PlaylistUrl  string  `json:"playlist_url"`
	ThumbnailUrl string  `json:"thumbnail_url"`
	StartAt      int64   `json:"start_at"`
	EndAt        int64   `json:"end_at"`
}

type LivestreamViewerModel struct {
	UserID       int64 `db:"user_id" json:"user_id"`
	LivestreamID int64 `db:"livestream_id" json:"livestream_id"`
	CreatedAt    int64 `db:"created_at" json:"created_at"`
}

type LivestreamModel struct {
	ID           int64  `db:"id" json:"id"`
	UserID       int64  `db:"user_id" json:"user_id"`
	Title        string `db:"title" json:"title"`
	Description  string `db:"description" json:"description"`
	PlaylistUrl  string `db:"playlist_url" json:"playlist_url"`
	ThumbnailUrl string `db:"thumbnail_url" json:"thumbnail_url"`
	StartAt      int64  `db:"start_at" json:"start_at"`
	EndAt        int64  `db:"end_at" json:"end_at"`

	TagIds Int64ArrayJson `db:"tag_ids" json:"-"`
}

type Livestream struct {
	ID           int64  `json:"id"`
	Owner        User   `json:"owner"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	PlaylistUrl  string `json:"playlist_url"`
	ThumbnailUrl string `json:"thumbnail_url"`
	Tags         []Tag  `json:"tags"`
	StartAt      int64  `json:"start_at"`
	EndAt        int64  `json:"end_at"`
}

type LivestreamTagModel struct {
	LivestreamID int64 `db:"livestream_id" json:"livestream_id"`
	TagID        int64 `db:"tag_id" json:"tag_id"`
}

type ReservationSlotModel struct {
	ID      int64 `db:"id" json:"id"`
	Slot    int64 `db:"slot" json:"slot"`
	StartAt int64 `db:"start_at" json:"start_at"`
	EndAt   int64 `db:"end_at" json:"end_at"`
}

func reserveLivestreamHandler(c echo.Context) error {
	ctx := c.Request().Context()
	defer c.Request().Body.Close()

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	var req *ReserveLivestreamRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	// 2023/11/25 10:00からの１年間の期間内であるかチェック
	var (
		termStartAt    = time.Date(2023, 11, 25, 1, 0, 0, 0, time.UTC)
		termEndAt      = time.Date(2024, 11, 25, 1, 0, 0, 0, time.UTC)
		reserveStartAt = time.Unix(req.StartAt, 0)
		reserveEndAt   = time.Unix(req.EndAt, 0)
	)
	if (reserveStartAt.Equal(termEndAt) || reserveStartAt.After(termEndAt)) || (reserveEndAt.Equal(termStartAt) || reserveEndAt.Before(termStartAt)) {
		return echo.NewHTTPError(http.StatusBadRequest, "bad reservation time range")
	}

	// 予約枠をみて、予約が可能か調べる
	// NOTE: 並列な予約のoverbooking防止にFOR UPDATEが必要
	var numEmptySlot int
	if err := tx.GetContext(ctx, &numEmptySlot, "SELECT COUNT(*) FROM reservation_slots WHERE (start_at BETWEEN ? AND ?) AND end_at <= ? AND slot = 0 FOR UPDATE", req.StartAt, req.EndAt, req.EndAt); err != nil {
		c.Logger().Warnf("予約枠一覧取得でエラー発生: %+v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get reservation_slots: "+err.Error())
	}
	if numEmptySlot > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "予約できません")
	}
	// ロックを解放するためにここでいったんcommitしておく
	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
	}

	tagIds := Int64ArrayJson(req.Tags)
	var (
		livestreamModel = &LivestreamModel{
			UserID:       int64(userID),
			Title:        req.Title,
			Description:  req.Description,
			PlaylistUrl:  req.PlaylistUrl,
			ThumbnailUrl: req.ThumbnailUrl,
			StartAt:      req.StartAt,
			EndAt:        req.EndAt,
			TagIds:       tagIds,
		}
	)

	tx2, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx2.Rollback()

	if _, err := tx2.ExecContext(ctx, "UPDATE reservation_slots SET slot = slot - 1 WHERE (start_at BETWEEN ? AND ?) AND end_at <= ?", req.StartAt, req.EndAt, req.EndAt); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update reservation_slot: "+err.Error())
	}

	rs, err := tx2.NamedExecContext(ctx, "INSERT INTO livestreams (user_id, title, description, playlist_url, thumbnail_url, tag_ids, start_at, end_at) VALUES(:user_id, :title, :description, :playlist_url, :thumbnail_url, :tag_ids, :start_at, :end_at)", livestreamModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert livestream: "+err.Error())
	}

	livestreamID, err := rs.LastInsertId()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get last inserted livestream id: "+err.Error())
	}
	livestreamModel.ID = livestreamID

	ltModels := godash.Map(req.Tags, func(tagID int64, _ int) *LivestreamTagModel {
		return &LivestreamTagModel{
			LivestreamID: livestreamID,
			TagID:        tagID,
		}
	})
	// タグ追加
	if len(ltModels) > 0 {
		if _, err := tx2.NamedExecContext(ctx, "INSERT INTO livestream_tags (livestream_id, tag_id) VALUES (:livestream_id, :tag_id)", ltModels); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert livestream tag: "+err.Error())
		}
	}

	livestream, err := fillLivestreamResponse(ctx, tx2, *livestreamModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill livestream: "+err.Error())
	}

	if err := tx2.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusCreated, livestream)
}

func searchLivestreamsHandler(c echo.Context) error {
	ctx := c.Request().Context()
	keyTagName := c.QueryParam("tag")

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	var livestreamModels []*LivestreamModel
	if c.QueryParam("tag") != "" {
		// タグによる取得
		tagIDList := []int{int(tagByName[keyTagName].ID)}
		query, params, err := sqlx.In("SELECT * FROM livestream_tags WHERE tag_id IN (?)", tagIDList)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to construct IN query for livestream_tags: "+err.Error())
		}
		var keyTaggedLivestreams []*LivestreamTagModel
		if err := tx.SelectContext(ctx, &keyTaggedLivestreams, query, params...); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get keyTaggedLivestreams: "+err.Error())
		}

		if len(keyTaggedLivestreams) > 0 {
			livestreamIds := make([]int64, len(keyTaggedLivestreams))
			for i, keyTaggedLivestream := range keyTaggedLivestreams {
				livestreamIds[i] = keyTaggedLivestream.LivestreamID
			}

			query, args, err := sqlx.In("SELECT * FROM livestreams WHERE id IN (?) ORDER BY id DESC", livestreamIds)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to construct IN query for livestreams: "+err.Error())
			}

			if err := tx.SelectContext(ctx, &livestreamModels, query, args...); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get livestreamModels: "+err.Error())
			}
		}
	} else {
		// 検索条件なし
		query := `SELECT * FROM livestreams ORDER BY id DESC`
		if c.QueryParam("limit") != "" {
			limit, err := strconv.Atoi(c.QueryParam("limit"))
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "limit query parameter must be integer")
			}
			query += fmt.Sprintf(" LIMIT %d", limit)
		}

		if err := tx.SelectContext(ctx, &livestreamModels, query); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get livestreams: "+err.Error())
		}
	}

	livestreams, err := bulkFillLivestreamResponse(ctx, tx, livestreamModels)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to bulkFillLivestreamRersponse: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, livestreams)
}

func getMyLivestreamsHandler(c echo.Context) error {
	ctx := c.Request().Context()
	if err := verifyUserSession(c); err != nil {
		return err
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	var livestreamModels []*LivestreamModel
	if err := tx.SelectContext(ctx, &livestreamModels, "SELECT * FROM livestreams WHERE user_id = ?", userID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get livestreams: "+err.Error())
	}
	livestreams, err := bulkFillLivestreamResponse(ctx, tx, livestreamModels)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill livestream: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, livestreams)
}

func getUserLivestreamsHandler(c echo.Context) error {
	ctx := c.Request().Context()
	if err := verifyUserSession(c); err != nil {
		return err
	}

	username := c.Param("username")

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	var user UserModel
	if err := tx.GetContext(ctx, &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		} else {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
		}
	}

	var livestreamModels []*LivestreamModel
	if err := tx.SelectContext(ctx, &livestreamModels, "SELECT * FROM livestreams WHERE user_id = ?", user.ID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get livestreams: "+err.Error())
	}
	livestreams, err := bulkFillLivestreamResponse(ctx, tx, livestreamModels)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill livestream: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, livestreams)
}

// viewerテーブルの廃止
func enterLivestreamHandler(c echo.Context) error {
	ctx := c.Request().Context()
	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	livestreamID, err := strconv.Atoi(c.Param("livestream_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "livestream_id must be integer")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	viewer := LivestreamViewerModel{
		UserID:       int64(userID),
		LivestreamID: int64(livestreamID),
		CreatedAt:    time.Now().Unix(),
	}

	if _, err := tx.NamedExecContext(ctx, "INSERT INTO livestream_viewers_history (user_id, livestream_id, created_at) VALUES(:user_id, :livestream_id, :created_at)", viewer); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert livestream_view_history: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.NoContent(http.StatusOK)
}

func exitLivestreamHandler(c echo.Context) error {
	ctx := c.Request().Context()
	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	livestreamID, err := strconv.Atoi(c.Param("livestream_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "livestream_id in path must be integer")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM livestream_viewers_history WHERE user_id = ? AND livestream_id = ?", userID, livestreamID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete livestream_view_history: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.NoContent(http.StatusOK)
}

func getLivestreamHandler(c echo.Context) error {
	ctx := c.Request().Context()

	if err := verifyUserSession(c); err != nil {
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

	livestreamModel := LivestreamModel{}
	err = tx.GetContext(ctx, &livestreamModel, "SELECT * FROM livestreams WHERE id = ?", livestreamID)
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "not found livestream that has the given id")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get livestream: "+err.Error())
	}

	livestream, err := fillLivestreamResponse(ctx, tx, livestreamModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill livestream: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, livestream)
}

func getLivecommentReportsHandler(c echo.Context) error {
	ctx := c.Request().Context()

	if err := verifyUserSession(c); err != nil {
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

	var livestreamModel LivestreamModel
	if err := tx.GetContext(ctx, &livestreamModel, "SELECT * FROM livestreams WHERE id = ?", livestreamID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get livestream: "+err.Error())
	}

	// error already check
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already check
	userID := sess.Values[defaultUserIDKey].(int64)

	if livestreamModel.UserID != userID {
		return echo.NewHTTPError(http.StatusForbidden, "can't get other streamer's livecomment reports")
	}

	var reportModels []*LivecommentReportModel
	if err := tx.SelectContext(ctx, &reportModels, "SELECT * FROM livecomment_reports WHERE livestream_id = ?", livestreamID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get livecomment reports: "+err.Error())
	}

	reports, err := bulkFillLivestreamCommentResponse(ctx, tx, reportModels)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to bulkFillLivestreamCommentResponse: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, reports)
}

func bulkFillLivestreamCommentResponse(ctx context.Context, tx *sqlx.Tx, reportModels []*LivecommentReportModel) ([]LivecommentReport, error) {
	if len(reportModels) == 0 {
		return []LivecommentReport{}, nil
	}

	userModels := []UserModel{}
	{
		userIds := godash.Map(reportModels, func(r *LivecommentReportModel, _ int) int64 { return r.UserID })
		query, args, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", userIds)
		if err != nil {
			return nil, fmt.Errorf("failed to construct IN query for users: %w", err)
		}
		if err := sqlx.SelectContext(ctx, tx, &userModels, query, args...); err != nil {
			return nil, fmt.Errorf("failed to query users: %w", err)
		}
	}
	userById, err := bulkFillUserResponse(ctx, tx, userModels)
	if err != nil {
		return nil, fmt.Errorf("bulkFillUserResponse: %w", err)
	}
	livecommentModels := []LivecommentModel{}
	{
		lcIds := godash.Map(reportModels, func(r *LivecommentReportModel, _ int) int64 { return r.LivecommentID })
		query, args, err := sqlx.In("SELECT * FROM livecomments WHERE id IN (?)", lcIds)
		if err != nil {
			return nil, fmt.Errorf("failed to construct IN query for users: %w", err)
		}
		if err := sqlx.SelectContext(ctx, tx, &livecommentModels, query, args...); err != nil {
			return nil, fmt.Errorf("failed to query users: %w", err)
		}
	}
	livecomments, err := bulkFillLivecommentResponse(ctx, tx, livecommentModels)
	if err != nil {
		return nil, fmt.Errorf("bulkFillLivecommentResponse: %w", err)
	}
	livecommentById := godash.KeyBy(livecomments, func(lc Livecomment) int64 { return lc.ID })

	reports := make([]LivecommentReport, len(reportModels))
	for i, reportModel := range reportModels {
		reporter := userById[reportModel.UserID]
		livecomment := livecommentById[reportModel.LivecommentID]

		report := LivecommentReport{
			ID:          reportModel.ID,
			Reporter:    reporter,
			Livecomment: livecomment,
			CreatedAt:   reportModel.CreatedAt,
		}
		reports[i] = report
	}
	return reports, nil
}

func bulkFillLivestreamResponse(ctx context.Context, tx sqlx.QueryerContext, livestreamModels []*LivestreamModel) ([]Livestream, error) {
	if len(livestreamModels) == 0 {
		return nil, nil
	}

	livestreams := make([]Livestream, len(livestreamModels))
	userIds := make([]int64, len(livestreamModels))
	for i, livestreamModel := range livestreamModels {
		userIds[i] = livestreamModel.UserID
	}

	userModels := []UserModel{}
	{
		query, args, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", userIds)
		if err != nil {
			return nil, fmt.Errorf("failed to construct IN query for users: %w", err)
		}
		if err := sqlx.SelectContext(ctx, tx, &userModels, query, args...); err != nil {
			return nil, fmt.Errorf("failed to query users: %w", err)
		}
	}
	userById, err := bulkFillUserResponse(ctx, tx, userModels)
	if err != nil {
		return nil, fmt.Errorf("bulkFillUserResponse: %w", err)
	}
	tagsByLivestreamId, err := bulkGetTagsByLivestream(ctx, tx, livestreamModels)
	if err != nil {
		return nil, fmt.Errorf("bulkGetTagsByLivestream: %w", err)
	}

	for i, livestreamModel := range livestreamModels {
		owner, ok := userById[livestreamModel.UserID]
		if !ok {
			return nil, fmt.Errorf("owner not found (id=%d)", livestreamModel.UserID)
		}

		tags := tagsByLivestreamId[livestreamModel.ID]

		livestream := Livestream{
			ID:           livestreamModel.ID,
			Owner:        owner,
			Title:        livestreamModel.Title,
			Tags:         tags,
			Description:  livestreamModel.Description,
			PlaylistUrl:  livestreamModel.PlaylistUrl,
			ThumbnailUrl: livestreamModel.ThumbnailUrl,
			StartAt:      livestreamModel.StartAt,
			EndAt:        livestreamModel.EndAt,
		}

		livestreams[i] = livestream
	}

	return livestreams, nil
}

func fillLivestreamResponse(ctx context.Context, tx *sqlx.Tx, livestreamModel LivestreamModel) (Livestream, error) {
	livestreams, err := bulkFillLivestreamResponse(ctx, tx, []*LivestreamModel{&livestreamModel})
	if err != nil {
		return Livestream{}, err
	}

	return livestreams[0], nil
}
