package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	hserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/store"
)

type Server struct {
	h             *hserver.Hertz
	library       *library.Service
	writer        *filewrite.Writer
	providers     *providers.Registry
	store         *store.Store
	frontend      fs.FS
	version       string
	tagEngineInfo string
}

func New(listen string, libraryService *library.Service, writer *filewrite.Writer, providerRegistry *providers.Registry, dataStore *store.Store, frontend fs.FS, version, tagEngineInfo string) *Server {
	h := hserver.Default(
		hserver.WithHostPorts(listen),
		hserver.WithMaxRequestBodySize(artwork.MaxBytes+1<<20),
	)
	s := &Server{
		h:             h,
		library:       libraryService,
		writer:        writer,
		providers:     providerRegistry,
		store:         dataStore,
		frontend:      frontend,
		version:       version,
		tagEngineInfo: tagEngineInfo,
	}
	s.routes()
	return s
}

func (s *Server) Spin() { s.h.Spin() }

func (s *Server) Shutdown(ctx context.Context) error { return s.h.Shutdown(ctx) }

func (s *Server) routes() {
	s.h.GET("/healthz", s.handleHealth)
	s.h.GET("/readyz", s.handleHealth)

	api := s.h.Group("/api/v1")
	api.GET("/system", s.handleSystem)
	api.GET("/libraries", s.handleLibraries)
	api.POST("/libraries/:id/scans", s.handleRescan)
	api.GET("/tracks", s.handleTracks)
	api.GET("/tracks/:id", s.handleTrack)
	api.PATCH("/tracks/:id/tags", s.handleWriteTags)
	api.GET("/tracks/:id/artwork/:index", s.handleReadArtwork)
	api.PUT("/tracks/:id/artwork/:index", s.handleWriteArtwork)
	api.DELETE("/tracks/:id/artwork/:index", s.handleDeleteArtwork)
	api.GET("/providers", s.handleProviders)
	api.POST("/matches/tracks/search", s.handleMatchSearch)
	api.GET("/revisions", s.handleRevisions)
	api.GET("/revisions/:id", s.handleRevision)
	api.POST("/revisions/:id/restore-preview", s.handleRevisionRestorePreview)
	api.POST("/revisions/:id/restore", s.handleRevisionRestore)

	s.h.GET("/", s.handleIndex)
	s.h.GET("/assets/*filepath", s.handleAsset)
	s.h.NoRoute(s.handleSPAFallback)
}

func (s *Server) handleHealth(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSystem(_ context.Context, c *app.RequestContext) {
	s.writeData(c, map[string]string{
		"version":    s.version,
		"tag_engine": s.tagEngineInfo,
	})
}

func (s *Server) handleLibraries(_ context.Context, c *app.RequestContext) {
	s.writeData(c, []domain.LibrarySummary{s.library.Library()})
}

func (s *Server) handleRescan(ctx context.Context, c *app.RequestContext) {
	librarySummary := s.library.Library()
	if c.Param("id") != librarySummary.ID {
		s.writeError(c, consts.StatusNotFound, "library_not_found", "曲库不存在")
		return
	}
	if err := s.library.Rescan(ctx); err != nil {
		s.writeError(c, consts.StatusInternalServerError, "scan_failed", err.Error())
		return
	}
	s.writeData(c, map[string]any{
		"library": s.library.Library(),
		"report":  s.library.LastReport(),
	})
}

func (s *Server) handleTracks(_ context.Context, c *app.RequestContext) {
	filter := library.TrackFilter{
		FolderID: c.Query("folder_id"),
		Query:    c.Query("q"),
		Health:   domain.TrackHealth(c.Query("health")),
		Format:   domain.TrackFormat(c.Query("format")),
	}
	tracks := s.library.ListTracks(filter)
	s.writeData(c, map[string]any{
		"tracks": tracks,
		"total":  len(tracks),
	})
}

func (s *Server) handleTrack(_ context.Context, c *app.RequestContext) {
	track, err := s.library.Track(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, track)
}

type tagWriteRequest struct {
	BaseRevision string          `json:"baseRevision"`
	Patch        domain.TagPatch `json:"patch"`
	DryRun       bool            `json:"dryRun"`
	Provenance   *struct {
		ProviderID string `json:"providerId"`
	} `json:"provenance,omitempty"`
}

func (s *Server) handleWriteTags(ctx context.Context, c *app.RequestContext) {
	if s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "write_unavailable", "标签写入服务尚未启用")
		return
	}
	var request tagWriteRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	headerRevision := strings.Trim(strings.TrimSpace(string(c.Request.Header.Peek("If-Match"))), `"`)
	if request.BaseRevision == "" {
		request.BaseRevision = headerRevision
	}
	if request.BaseRevision == "" || (headerRevision != "" && headerRevision != request.BaseRevision) {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "baseRevision 与 If-Match 必须一致且不能为空")
		return
	}
	action, source := "修改标签", "手工编辑"
	if request.Provenance != nil {
		descriptor, found := providers.Descriptor{}, false
		if s.providers != nil {
			descriptor, found = s.providers.Descriptor(strings.TrimSpace(request.Provenance.ProviderID))
		}
		if !found {
			s.writeError(c, consts.StatusBadRequest, "invalid_provenance", "数据来源未注册")
			return
		}
		action, source = "采用数据源元数据", descriptor.Name
	}
	ref, err := s.library.FileRef(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	result, err := s.writer.Write(ctx, ref, request.BaseRevision, request.Patch, request.DryRun)
	if err != nil {
		s.handleWriteError(c, err)
		return
	}
	if request.DryRun || !result.Changed {
		s.writeData(c, map[string]any{"preview": result})
		return
	}
	if err := s.library.Rescan(ctx); err != nil {
		s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "文件已写入，但重新索引失败："+err.Error())
		return
	}
	track, err := s.library.Track(ref.ID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "文件已写入，但曲目索引不可用")
		return
	}
	if s.store != nil {
		_, historyErr := s.store.CreateRevision(ctx, domain.Revision{
			LibraryID:      s.library.Library().ID,
			TrackID:        track.ID,
			TrackTitle:     track.Title,
			FileName:       track.FileName,
			Action:         action,
			Source:         source,
			BaseRevision:   result.BaseRevision,
			ResultRevision: track.Revision,
			Diff:           result.Diff,
			CoverTone:      track.CoverTone,
			BeforeTags:     result.BeforeTags,
			AfterTags:      result.AfterTags,
		})
		if historyErr != nil {
			result.Warnings = append(result.Warnings, "修订历史写入失败："+historyErr.Error())
		}
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, map[string]any{"track": track, "write": result})
}

type revisionResponse struct {
	ID              string                `json:"id"`
	TrackID         string                `json:"trackId"`
	TrackTitle      string                `json:"trackTitle"`
	FileName        string                `json:"fileName"`
	Action          string                `json:"action"`
	Source          string                `json:"source"`
	Time            string                `json:"time"`
	Fields          []string              `json:"fields"`
	Diff            []domain.RevisionDiff `json:"diff"`
	CoverTone       domain.CoverTone      `json:"coverTone"`
	BaseRevision    string                `json:"baseRevision"`
	ResultRevision  string                `json:"resultRevision"`
	CurrentRevision string                `json:"currentRevision,omitempty"`
}

func (s *Server) handleRevisions(ctx context.Context, c *app.RequestContext) {
	if s.store == nil {
		s.writeData(c, []revisionResponse{})
		return
	}
	revisions, err := s.store.ListRevisions(ctx, 100)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "history_failed", err.Error())
		return
	}
	result := make([]revisionResponse, len(revisions))
	for index, revision := range revisions {
		result[index] = toRevisionResponse(revision, s.trackRevision(revision.TrackID))
	}
	s.writeData(c, result)
}

func (s *Server) handleRevision(ctx context.Context, c *app.RequestContext) {
	if s.store == nil {
		s.writeError(c, consts.StatusNotFound, "revision_not_found", "修订记录不存在")
		return
	}
	revision, err := s.store.Revision(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "revision_not_found", "修订记录不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "history_failed", err.Error())
		return
	}
	s.writeData(c, toRevisionResponse(revision, s.trackRevision(revision.TrackID)))
}

func toRevisionResponse(revision domain.Revision, currentRevision string) revisionResponse {
	return revisionResponse{
		ID: revision.ID, TrackID: revision.TrackID, TrackTitle: revision.TrackTitle,
		FileName: revision.FileName, Action: revision.Action, Source: revision.Source,
		Time: revision.CreatedAt.Local().Format("2006-01-02 15:04"), Fields: revision.Fields,
		Diff: revision.Diff, CoverTone: revision.CoverTone, BaseRevision: revision.BaseRevision,
		ResultRevision: revision.ResultRevision, CurrentRevision: currentRevision,
	}
}

func (s *Server) trackRevision(trackID string) string {
	track, err := s.library.Track(trackID)
	if err != nil {
		return ""
	}
	return track.Revision
}

type revisionRestoreRequest struct {
	BaseRevision string `json:"baseRevision"`
	Target       string `json:"target"`
}

func (s *Server) handleRevisionRestorePreview(ctx context.Context, c *app.RequestContext) {
	s.handleRevisionRestoreRequest(ctx, c, true)
}

func (s *Server) handleRevisionRestore(ctx context.Context, c *app.RequestContext) {
	s.handleRevisionRestoreRequest(ctx, c, false)
}

func (s *Server) handleRevisionRestoreRequest(ctx context.Context, c *app.RequestContext, dryRun bool) {
	if s.store == nil || s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "restore_unavailable", "历史恢复服务尚未启用")
		return
	}
	var request revisionRestoreRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	headerRevision := strings.Trim(strings.TrimSpace(string(c.Request.Header.Peek("If-Match"))), `"`)
	if request.BaseRevision == "" {
		request.BaseRevision = headerRevision
	}
	if request.BaseRevision == "" || (headerRevision != "" && headerRevision != request.BaseRevision) {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "baseRevision 与 If-Match 必须一致且不能为空")
		return
	}
	if request.Target == "" {
		request.Target = "before"
	}
	if request.Target != "before" && request.Target != "after" {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "target 必须是 before 或 after")
		return
	}
	revision, err := s.store.Revision(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "revision_not_found", "修订记录不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "history_failed", err.Error())
		return
	}
	if revision.LibraryID != s.library.Library().ID {
		s.writeError(c, consts.StatusNotFound, "revision_not_found", "修订记录不存在")
		return
	}
	ref, err := s.library.FileRef(revision.TrackID)
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "修订对应的曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	targetTags := revision.BeforeTags
	if request.Target == "after" {
		targetTags = revision.AfterTags
	}
	result, err := s.writer.Restore(ctx, ref, request.BaseRevision, targetTags, dryRun)
	if err != nil {
		s.handleWriteError(c, err)
		return
	}
	if dryRun {
		s.writeData(c, map[string]any{
			"revisionId": revision.ID, "trackId": revision.TrackID, "target": request.Target, "preview": result,
		})
		return
	}
	if result.Changed {
		if err := s.library.Rescan(ctx); err != nil {
			s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "文件已恢复，但重新索引失败："+err.Error())
			return
		}
	}
	track, err := s.library.Track(revision.TrackID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "文件已恢复，但曲目索引不可用")
		return
	}
	if result.Changed {
		action := "恢复到修订前"
		if request.Target == "after" {
			action = "恢复到修订后"
		}
		_, historyErr := s.store.CreateRevision(ctx, domain.Revision{
			LibraryID: s.library.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName,
			Action: action, Source: "历史修订 " + revision.ID, BaseRevision: result.BaseRevision,
			ResultRevision: track.Revision, Diff: result.Diff, CoverTone: track.CoverTone,
			BeforeTags: result.BeforeTags, AfterTags: result.AfterTags,
		})
		if historyErr != nil {
			result.Warnings = append(result.Warnings, "修订历史写入失败："+historyErr.Error())
		}
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, map[string]any{
		"track": track, "write": result, "restoredRevisionId": revision.ID, "target": request.Target,
	})
}

func (s *Server) handleWriteError(c *app.RequestContext, err error) {
	var conflict *filewrite.RevisionConflictError
	switch {
	case errors.As(err, &conflict):
		c.JSON(consts.StatusConflict, map[string]any{
			"error": map[string]any{
				"code":    "revision_conflict",
				"message": "文件已被其他操作修改",
				"details": map[string]string{"current_revision": conflict.Current},
			},
		})
	case errors.Is(err, filewrite.ErrInvalidPatch):
		s.writeError(c, consts.StatusBadRequest, "invalid_patch", err.Error())
	case errors.Is(err, filewrite.ErrPathOutsideRoot):
		s.writeError(c, consts.StatusForbidden, "forbidden", "文件路径不在曲库安全边界内")
	case errors.Is(err, filewrite.ErrUnsupportedFormat):
		s.writeError(c, consts.StatusUnprocessableEntity, "unwritable_format", "当前格式不支持写入")
	case errors.Is(err, filewrite.ErrVerification):
		s.writeError(c, consts.StatusInternalServerError, "write_verification_failed", err.Error())
	case errors.Is(err, filewrite.ErrArtworkUnavailable):
		s.writeError(c, consts.StatusServiceUnavailable, "artwork_unavailable", "当前标签引擎不支持封面操作")
	case errors.Is(err, filewrite.ErrArtworkNotFound):
		s.writeError(c, consts.StatusNotFound, "artwork_not_found", "嵌入封面不存在")
	case errors.Is(err, filewrite.ErrArtworkIndex):
		s.writeError(c, consts.StatusBadRequest, "invalid_artwork_index", "封面序号无效")
	default:
		s.writeError(c, consts.StatusInternalServerError, "write_failed", err.Error())
	}
}

func (s *Server) handleReadArtwork(ctx context.Context, c *app.RequestContext) {
	if s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "artwork_unavailable", "封面读取服务尚未启用")
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 || index > 31 {
		s.writeError(c, consts.StatusBadRequest, "invalid_artwork_index", "封面序号无效")
		return
	}
	ref, err := s.library.FileRef(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	asset, err := s.writer.ReadArtwork(ctx, ref, index)
	if err != nil {
		s.handleWriteError(c, err)
		return
	}
	c.Header("ETag", `"`+ref.Revision+`"`)
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Tagger-Artwork-Hash", asset.Hash)
	c.Header("X-Tagger-Artwork-Size", fmt.Sprintf("%dx%d", asset.Width, asset.Height))
	c.Data(consts.StatusOK, asset.MIME, asset.Data)
}

func (s *Server) handleWriteArtwork(ctx context.Context, c *app.RequestContext) {
	asset, err := artwork.Validate(c.Request.Body(), string(c.Request.Header.ContentType()))
	if err != nil {
		s.writeError(c, consts.StatusUnprocessableEntity, "invalid_artwork", err.Error())
		return
	}
	s.handleArtworkMutation(ctx, c, &asset)
}

func (s *Server) handleDeleteArtwork(ctx context.Context, c *app.RequestContext) {
	s.handleArtworkMutation(ctx, c, nil)
}

func (s *Server) handleArtworkMutation(ctx context.Context, c *app.RequestContext, target *artwork.Asset) {
	if s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "artwork_unavailable", "封面写入服务尚未启用")
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 || index > 31 {
		s.writeError(c, consts.StatusBadRequest, "invalid_artwork_index", "封面序号无效")
		return
	}
	baseRevision := strings.Trim(strings.TrimSpace(string(c.Request.Header.Peek("If-Match"))), `"`)
	if baseRevision == "" {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "If-Match 不能为空")
		return
	}
	ref, err := s.library.FileRef(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	dryRun := strings.EqualFold(c.Query("dry_run"), "true")
	result, err := s.writer.WriteArtwork(ctx, ref, baseRevision, index, target, dryRun)
	if err != nil {
		s.handleWriteError(c, err)
		return
	}
	if dryRun {
		s.writeData(c, map[string]any{"preview": result})
		return
	}
	if result.Changed {
		if err := s.library.Rescan(ctx); err != nil {
			s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "封面已写入，但重新索引失败："+err.Error())
			return
		}
	}
	track, err := s.library.Track(ref.ID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "封面已写入，但曲目索引不可用")
		return
	}
	if result.Changed && s.store != nil {
		action, source := "替换封面", "手工上传"
		if target == nil {
			action, source = "删除封面", "手工操作"
		}
		_, historyErr := s.store.CreateRevision(ctx, domain.Revision{
			LibraryID: s.library.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName,
			Action: action, Source: source, BaseRevision: result.BaseRevision, ResultRevision: track.Revision,
			Diff: result.Diff, CoverTone: track.CoverTone, BeforeTags: result.BeforeTags, AfterTags: result.AfterTags,
		})
		if historyErr != nil {
			result.Warnings = append(result.Warnings, "修订历史写入失败："+historyErr.Error())
		}
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, map[string]any{"track": track, "write": result})
}

func (s *Server) handleProviders(_ context.Context, c *app.RequestContext) {
	if s.providers == nil {
		s.writeData(c, []providers.Descriptor{})
		return
	}
	s.writeData(c, s.providers.Descriptors())
}

type matchSearchRequest struct {
	FileID string `json:"fileId"`
	Query  struct {
		Title           string   `json:"title"`
		Artists         []string `json:"artists"`
		Album           string   `json:"album"`
		DurationSeconds int64    `json:"durationSeconds"`
	} `json:"query"`
	ProviderIDs      []string `json:"providerIds"`
	LimitPerProvider int      `json:"limitPerProvider"`
}

func (s *Server) handleMatchSearch(ctx context.Context, c *app.RequestContext) {
	if s.providers == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "provider_unavailable", "抓取器尚未初始化")
		return
	}
	var request matchSearchRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	query := providers.Query{
		Title: request.Query.Title, Artists: request.Query.Artists, Album: request.Query.Album,
		DurationSeconds: request.Query.DurationSeconds,
	}
	if request.FileID != "" {
		track, err := s.library.Track(request.FileID)
		if errors.Is(err, library.ErrTrackNotFound) {
			s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
			return
		}
		if err != nil {
			s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if query.Title == "" {
			query.Title = track.Title
		}
		if len(query.Artists) == 0 {
			query.Artists = track.Artists
		}
		if query.Album == "" {
			query.Album = track.Album
		}
		if query.DurationSeconds == 0 {
			query.DurationSeconds = track.DurationSeconds
		}
	}
	result, err := s.providers.Search(ctx, query, request.ProviderIDs, request.LimitPerProvider)
	if errors.Is(err, providers.ErrProviderNotFound) {
		s.writeError(c, consts.StatusNotFound, "provider_not_found", err.Error())
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.writeData(c, result)
}

func (s *Server) handleIndex(_ context.Context, c *app.RequestContext) {
	s.serveFrontendFile(c, "index.html", false)
}

func (s *Server) handleAsset(_ context.Context, c *app.RequestContext) {
	path := strings.TrimPrefix(c.Param("filepath"), "/")
	if path == "" || strings.Contains(path, "..") || strings.ContainsRune(path, '\x00') {
		s.writeError(c, consts.StatusNotFound, "not_found", "资源不存在")
		return
	}
	s.serveFrontendFile(c, "assets/"+path, true)
}

func (s *Server) handleSPAFallback(_ context.Context, c *app.RequestContext) {
	path := string(c.Request.URI().Path())
	if string(c.Method()) != consts.MethodGet ||
		strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/assets/") ||
		path == "/healthz" || path == "/readyz" ||
		!strings.Contains(string(c.Request.Header.Peek("Accept")), "text/html") {
		s.writeError(c, consts.StatusNotFound, "not_found", "资源不存在")
		return
	}
	s.serveFrontendFile(c, "index.html", false)
}

func (s *Server) serveFrontendFile(c *app.RequestContext, name string, immutable bool) {
	data, err := fs.ReadFile(s.frontend, name)
	if err != nil {
		s.writeError(c, consts.StatusNotFound, "frontend_not_built", "前端资源尚未构建")
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if name == "index.html" {
		contentType = "text/html; charset=utf-8"
		c.Header("Cache-Control", "no-cache")
	} else if immutable {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
	c.Data(consts.StatusOK, contentType, data)
}

func (s *Server) writeData(c *app.RequestContext, data any) {
	c.JSON(consts.StatusOK, map[string]any{"data": data})
}

func (s *Server) writeError(c *app.RequestContext, status int, code, message string) {
	c.JSON(status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
