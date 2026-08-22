package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	hserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/jobs"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/store"
)

type Server struct {
	h               *hserver.Hertz
	library         *library.Service
	writer          *filewrite.Writer
	providers       *providers.Registry
	store           *store.Store
	jobs            *jobs.Manager
	frontend        fs.FS
	listen          string
	version         string
	tagEngineInfo   string
	authToken       string
	downloadArtwork func(context.Context, providers.ArtworkReference) (artwork.Asset, error)
}

const authCookieName = "tagger_auth_token"

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
		listen:        listen,
		version:       version,
		tagEngineInfo: tagEngineInfo,
		downloadArtwork: func(ctx context.Context, reference providers.ArtworkReference) (artwork.Asset, error) {
			return providers.DownloadArtwork(ctx, reference, nil)
		},
	}
	s.routes()
	return s
}

func (s *Server) Spin() { s.h.Spin() }

func (s *Server) Shutdown(ctx context.Context) error { return s.h.Shutdown(ctx) }

func (s *Server) SetJobManager(manager *jobs.Manager) { s.jobs = manager }

// SetAuthToken enables optional single-user bearer-token protection for API
// routes. An empty token deliberately keeps local development unauthenticated.
func (s *Server) SetAuthToken(token string) { s.authToken = strings.TrimSpace(token) }

func (s *Server) routes() {
	s.h.GET("/healthz", s.handleHealth)
	s.h.GET("/readyz", s.handleHealth)

	api := s.h.Group("/api/v1")
	api.Use(s.requireAuth)
	api.GET("/system", s.handleSystem)
	api.PATCH("/system/settings", s.handleSystemSettings)
	api.GET("/libraries", s.handleLibraries)
	api.POST("/libraries/probe", s.handleLibraryProbe)
	api.POST("/libraries/:id/switch", s.handleLibrarySwitch)
	api.POST("/libraries/:id/scans", s.handleRescan)
	api.GET("/tracks", s.handleTracks)
	api.POST("/tracks/:id/scan", s.handleTrackScan)
	api.POST("/tracks/batch-edit", s.handleBatchEdit)
	api.GET("/tracks/:id", s.handleTrack)
	api.GET("/tracks/:id/raw-tags", s.handleRawTags)
	api.GET("/tracks/:id/lyrics-sidecar", s.handleLyricsSidecarGet)
	api.PUT("/tracks/:id/lyrics-sidecar", s.handleLyricsSidecarPut)
	api.DELETE("/tracks/:id/lyrics-sidecar", s.handleLyricsSidecarDelete)
	api.GET("/tracks/:id/audio", s.handleAudio)
	api.PATCH("/tracks/:id/tags", s.handleWriteTags)
	api.GET("/tracks/:id/artwork/:index", s.handleReadArtwork)
	api.PUT("/tracks/:id/artwork/:index", s.handleWriteArtwork)
	api.DELETE("/tracks/:id/artwork/:index", s.handleDeleteArtwork)
	api.GET("/providers", s.handleProviders)
	api.PATCH("/providers/:id", s.handleProviderUpdate)
	api.POST("/providers/:id/test", s.handleProviderTest)
	api.POST("/matches/tracks/search", s.handleMatchSearch)
	api.GET("/matches/tracks/:id/query-history", s.handleMatchQueryHistory)
	api.POST("/matches/tracks/batch", s.handleMatchBatch)
	api.POST("/matches/jobs/:id/items/:trackId/rematch", s.handleMatchRematch)
	api.PATCH("/matches/jobs/:id/items/:trackId", s.handleMatchReviewUpdate)
	api.POST("/matches/jobs/:id/write", s.handleMatchWrite)
	api.POST("/matches/tracks/:id/artwork", s.handleMatchArtwork)
	api.GET("/matches/candidates/:id/artwork", s.handleCandidateArtwork)
	api.GET("/jobs", s.handleJobs)
	api.GET("/jobs/:id", s.handleJob)
	api.POST("/jobs/:id/cancel", s.handleCancelJob)
	api.POST("/jobs/:id/retry", s.handleRetryJob)
	api.GET("/jobs/:id/events", s.handleJobEvents)
	api.GET("/jobs/:id/matches", s.handleJobMatches)
	api.GET("/jobs/:id/batch-edit-items", s.handleBatchEditItems)
	api.GET("/revisions", s.handleRevisions)
	api.GET("/revisions/:id", s.handleRevision)
	api.POST("/revisions/:id/restore-preview", s.handleRevisionRestorePreview)
	api.POST("/revisions/:id/restore", s.handleRevisionRestore)

	s.h.GET("/", s.handleIndex)
	s.h.GET("/assets/*filepath", s.handleAsset)
	s.h.NoRoute(s.handleSPAFallback)
}

func (s *Server) requireAuth(ctx context.Context, c *app.RequestContext) {
	if s.authToken == "" {
		c.Next(ctx)
		return
	}
	authorization := strings.TrimSpace(string(c.Request.Header.Peek("Authorization")))
	token := ""
	fromHeader := false
	if fields := strings.Fields(authorization); len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		token = fields[1]
		fromHeader = true
	}
	if token == "" {
		token = strings.TrimSpace(string(c.Request.Header.Peek("X-Tagger-Token")))
		fromHeader = token != ""
	}
	if token == "" {
		encoded := strings.TrimSpace(string(c.Request.Header.Cookie(authCookieName)))
		if decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded); decodeErr == nil {
			token = string(decoded)
		}
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
		c.Header("WWW-Authenticate", `Bearer realm="tagger"`)
		c.AbortWithStatusJSON(consts.StatusUnauthorized, map[string]any{"error": map[string]string{
			"code": "auth_required", "message": "需要提供有效的 Tagger 访问令牌",
		}})
		return
	}
	if fromHeader {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(token))
		c.Header("Set-Cookie", authCookieName+"="+encoded+"; Path=/; HttpOnly; SameSite=Strict")
	}
	c.Next(ctx)
}

func (s *Server) handleHealth(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSystem(ctx context.Context, c *app.RequestContext) {
	historyRetention := 20
	if s.store != nil {
		historyRetention = s.store.HistoryRetention(ctx)
	}
	s.writeData(c, map[string]any{
		"version":          s.version,
		"tag_engine":       s.tagEngineInfo,
		"listen":           s.listen,
		"historyRetention": historyRetention,
	})
}

type systemSettingsRequest struct {
	HistoryRetention *int `json:"historyRetention"`
}

func (s *Server) handleSystemSettings(ctx context.Context, c *app.RequestContext) {
	if s.store == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "settings_unavailable", "系统设置存储尚未初始化")
		return
	}
	var request systemSettingsRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil || request.HistoryRetention == nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "需要提供 historyRetention")
		return
	}
	if err := s.store.SetHistoryRetention(ctx, *request.HistoryRetention); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_history_retention", err.Error())
		return
	}
	s.writeData(c, map[string]any{"historyRetention": s.store.HistoryRetention(ctx)})
}

func (s *Server) handleLibraries(_ context.Context, c *app.RequestContext) {
	s.writeData(c, []domain.LibrarySummary{s.library.Library()})
}

type libraryProbeRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleLibraryProbe(_ context.Context, c *app.RequestContext) {
	var request libraryProbeRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "目录探测 JSON 无效")
		return
	}
	probe, err := library.ProbeRoot(request.Path)
	if err != nil {
		s.writeError(c, consts.StatusUnprocessableEntity, "directory_probe_failed", err.Error())
		return
	}
	s.writeData(c, probe)
}

func (s *Server) handleLibrarySwitch(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "job_unavailable", "曲库切换任务队列尚未启用")
		return
	}
	current := s.library.Library()
	if c.Param("id") != current.ID {
		s.writeError(c, consts.StatusNotFound, "library_not_found", "曲库不存在")
		return
	}
	active, err := s.jobs.HasActive(ctx)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	if active {
		s.writeError(c, consts.StatusConflict, "library_switch_busy", "存在运行中或待审核任务，请完成或取消后再切换曲库")
		return
	}
	var request libraryProbeRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "曲库切换 JSON 无效")
		return
	}
	probe, err := library.ProbeRoot(request.Path)
	if err != nil {
		s.writeError(c, consts.StatusUnprocessableEntity, "directory_probe_failed", err.Error())
		return
	}
	payload, _ := json.Marshal(map[string]string{"root": probe.Path})
	job, err := s.jobs.Enqueue(ctx, domain.Job{
		Kind: domain.JobScan, LibraryID: current.ID, Title: "切换曲库 · " + probe.Name,
		Detail: "等待曲库切换 worker", Total: probe.AudioFiles, Payload: string(payload),
	})
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "job_enqueue_failed", err.Error())
		return
	}
	c.JSON(consts.StatusAccepted, map[string]any{"data": toJobResponse(job), "probe": probe})
}

func (s *Server) handleRescan(ctx context.Context, c *app.RequestContext) {
	librarySummary := s.library.Library()
	if c.Param("id") != librarySummary.ID {
		s.writeError(c, consts.StatusNotFound, "library_not_found", "曲库不存在")
		return
	}
	if s.jobs != nil {
		job, err := s.jobs.Enqueue(ctx, domain.Job{
			Kind: domain.JobScan, LibraryID: librarySummary.ID, Title: librarySummary.Name + " 曲库扫描",
			Detail: "等待扫描 worker", Total: librarySummary.TrackCount,
		})
		if err != nil {
			s.writeError(c, consts.StatusInternalServerError, "job_enqueue_failed", err.Error())
			return
		}
		c.JSON(consts.StatusAccepted, map[string]any{"data": toJobResponse(job)})
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

type jobResponse struct {
	ID        string          `json:"id"`
	Kind      domain.JobKind  `json:"kind"`
	State     domain.JobState `json:"state"`
	Title     string          `json:"title"`
	Detail    string          `json:"detail"`
	Processed int             `json:"processed"`
	Total     int             `json:"total"`
	Succeeded int             `json:"succeeded"`
	Failed    int             `json:"failed"`
	Error     string          `json:"error,omitempty"`
	StartedAt string          `json:"startedAt"`
}

func toJobResponse(job domain.Job) jobResponse {
	started := job.StartedAt
	if started.IsZero() {
		started = job.CreatedAt
	}
	return jobResponse{ID: job.ID, Kind: job.Kind, State: job.State, Title: job.Title, Detail: job.Detail,
		Processed: job.Processed, Total: job.Total, Succeeded: job.Succeeded, Failed: job.Failed,
		Error: job.Error, StartedAt: started.Local().Format("2006-01-02 15:04:05")}
}

func (s *Server) handleJobs(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil {
		s.writeData(c, []jobResponse{})
		return
	}
	items, err := s.jobs.List(ctx, 100)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	result := make([]jobResponse, len(items))
	for index, item := range items {
		result[index] = toJobResponse(item)
	}
	s.writeData(c, result)
}

func (s *Server) handleJob(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	job, err := s.jobs.Get(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	s.writeData(c, toJobResponse(job))
}

func (s *Server) handleCancelJob(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	job, err := s.jobs.Cancel(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	if errors.Is(err, jobs.ErrJobNotCancellable) {
		s.writeError(c, consts.StatusConflict, "job_not_cancellable", "当前任务状态不允许取消")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "job_cancel_failed", err.Error())
		return
	}
	s.writeData(c, toJobResponse(job))
}

func (s *Server) handleRetryJob(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil || s.store == nil {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	job, err := s.jobs.Get(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	payload, err := s.retryPayload(ctx, job)
	if errors.Is(err, jobs.ErrJobNotRetryable) {
		s.writeError(c, consts.StatusConflict, "job_not_retryable", "当前任务没有可重试的失败项")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "job_retry_failed", err.Error())
		return
	}
	retried, err := s.jobs.Retry(ctx, job.ID, payload)
	if errors.Is(err, jobs.ErrJobNotRetryable) {
		s.writeError(c, consts.StatusConflict, "job_not_retryable", "当前任务状态不允许重试")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "job_retry_failed", err.Error())
		return
	}
	c.JSON(consts.StatusAccepted, map[string]any{"data": toJobResponse(retried)})
}

func (s *Server) retryPayload(ctx context.Context, job domain.Job) (string, error) {
	switch job.Kind {
	case domain.JobScan:
		return "", nil
	case domain.JobMatch:
		var payload matchBatchRequest
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return "", fmt.Errorf("decode match retry payload: %w", err)
		}
		items, err := s.store.ListMatchItems(ctx, job.ID)
		if err != nil {
			return "", err
		}
		failed := make(map[string]struct{}, len(items))
		for _, item := range items {
			if item.State == "failed" || item.State == "no_match" {
				failed[item.TrackID] = struct{}{}
			}
		}
		trackIDs := make([]string, 0, len(payload.TrackIDs))
		for _, trackID := range payload.TrackIDs {
			if _, ok := failed[trackID]; ok {
				trackIDs = append(trackIDs, trackID)
			}
		}
		if len(trackIDs) == 0 {
			return "", jobs.ErrJobNotRetryable
		}
		payload.TrackIDs = trackIDs
		encoded, err := json.Marshal(payload)
		return string(encoded), err
	case domain.JobWrite:
		var payload struct {
			MatchJobID string           `json:"matchJobId"`
			Items      []writeSelection `json:"items"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return "", fmt.Errorf("decode write retry payload: %w", err)
		}
		failedItems, err := s.store.ListMatchItems(ctx, payload.MatchJobID)
		if err != nil {
			return "", err
		}
		failed := make(map[string]struct{}, len(failedItems))
		failedStates := make(map[string]string, len(failedItems))
		for _, item := range failedItems {
			if item.State == "write_failed" || item.State == "artwork_failed" {
				failedStates[item.TrackID] = item.State
				failed[item.TrackID] = struct{}{}
			}
		}
		items := make([]writeSelection, 0, len(payload.Items))
		for _, item := range payload.Items {
			if _, ok := failed[item.TrackID]; ok {
				if failedStates[item.TrackID] == "artwork_failed" {
					track, trackErr := s.library.Track(item.TrackID)
					if trackErr != nil {
						return "", trackErr
					}
					item.BaseRevision = track.Revision
					item.Fields = []string{}
					item.Artwork = true
				}
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			return "", jobs.ErrJobNotRetryable
		}
		payload.Items = items
		encoded, err := json.Marshal(payload)
		return string(encoded), err
	case domain.JobBatchEdit:
		var payload domain.BatchEditPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return "", fmt.Errorf("decode batch edit retry payload: %w", err)
		}
		failedItems, err := s.store.ListBatchEditItems(ctx, job.ID)
		if err != nil {
			return "", err
		}
		failed := make(map[string]struct{}, len(failedItems))
		for _, item := range failedItems {
			if item.State == "failed" {
				failed[item.TrackID] = struct{}{}
			}
		}
		items := make([]domain.BatchEditItem, 0, len(payload.Items))
		for _, item := range payload.Items {
			if _, ok := failed[item.TrackID]; !ok {
				continue
			}
			track, trackErr := s.library.Track(item.TrackID)
			if trackErr != nil {
				return "", trackErr
			}
			item.BaseRevision = track.Revision
			items = append(items, item)
		}
		if len(items) == 0 {
			return "", jobs.ErrJobNotRetryable
		}
		payload.Items = items
		encoded, err := json.Marshal(payload)
		return string(encoded), err
	default:
		return "", jobs.ErrJobNotRetryable
	}
}

func (s *Server) handleJobEvents(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	jobID := c.Param("id")
	if _, err := s.jobs.Get(ctx, jobID); errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "任务不存在")
		return
	} else if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	events, unsubscribe := s.jobs.Subscribe(jobID)
	defer unsubscribe()
	writer := sse.NewWriter(c)
	defer writer.Close()
	current, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return
	}
	if err := writeJobEvent(writer, jobs.Event{ID: "snapshot", Job: current}); err != nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeJobEvent(writer, event); err != nil {
				return
			}
		case <-ticker.C:
			if err := writer.WriteKeepAlive(); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func writeJobEvent(writer *sse.Writer, event jobs.Event) error {
	payload, err := json.Marshal(toJobResponse(event.Job))
	if err != nil {
		return err
	}
	return writer.WriteEvent(event.ID, "job", payload)
}

type matchBatchRequest struct {
	TrackIDs    []string `json:"trackIds"`
	ProviderIDs []string `json:"providerIds"`
	Limit       int      `json:"limit"`
}

func (s *Server) handleMatchBatch(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "job_unavailable", "任务队列尚未启用")
		return
	}
	var request matchBatchRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil || len(request.TrackIDs) == 0 {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "trackIds 不能为空且请求 JSON 必须有效")
		return
	}
	if len(request.TrackIDs) > 1000 {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "单次最多处理 1000 首曲目")
		return
	}
	payload, _ := json.Marshal(request)
	job, err := s.jobs.Enqueue(ctx, domain.Job{Kind: domain.JobMatch, LibraryID: s.library.Library().ID, Title: "批量抓取元数据", Detail: "等待匹配 worker", Total: len(request.TrackIDs), Payload: string(payload)})
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "job_enqueue_failed", err.Error())
		return
	}
	c.JSON(consts.StatusAccepted, map[string]any{"data": toJobResponse(job)})
}

func (s *Server) handleJobMatches(ctx context.Context, c *app.RequestContext) {
	if s.store == nil {
		s.writeData(c, []store.MatchItem{})
		return
	}
	items, err := s.store.ListMatchItems(ctx, c.Param("id"))
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "matches_failed", err.Error())
		return
	}
	s.writeData(c, items)
}

type matchReviewUpdateRequest struct {
	State               string   `json:"state"`
	SelectedCandidateID string   `json:"selectedCandidateId,omitempty"`
	Fields              []string `json:"fields,omitempty"`
	Artwork             *bool    `json:"artwork,omitempty"`
	ArtworkMaxSize      *int     `json:"artworkMaxSize,omitempty"`
}

type matchQueryFields struct {
	Title           string   `json:"title"`
	Artists         []string `json:"artists"`
	Album           string   `json:"album"`
	DurationSeconds int64    `json:"durationSeconds"`
}

type matchRematchRequest struct {
	Query            matchQueryFields `json:"query"`
	ProviderIDs      []string         `json:"providerIds"`
	LimitPerProvider int              `json:"limitPerProvider"`
}

func (s *Server) handleMatchRematch(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil || s.store == nil || s.providers == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "job_unavailable", "审核重新匹配服务尚未启用")
		return
	}
	job, err := s.jobs.Get(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "匹配任务不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	if job.Kind != domain.JobMatch || (job.State != domain.JobReview && job.State != domain.JobPartial) {
		s.writeError(c, consts.StatusConflict, "job_not_reviewable", "匹配任务当前不允许重新匹配")
		return
	}
	var request matchRematchRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	track, err := s.library.Track(c.Param("trackId"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "track_failed", err.Error())
		return
	}
	query := providers.Query{
		Title: request.Query.Title, Artists: request.Query.Artists, Album: request.Query.Album,
		DurationSeconds: request.Query.DurationSeconds,
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
	result, err := s.providers.Search(ctx, query, request.ProviderIDs, request.LimitPerProvider)
	if errors.Is(err, providers.ErrProviderNotFound) {
		s.writeError(c, consts.StatusNotFound, "provider_not_found", err.Error())
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusBadRequest, "rematch_failed", err.Error())
		return
	}
	item, err := s.store.MatchItem(ctx, job.ID, track.ID)
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "match_item_not_found", "审核曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "match_state_failed", err.Error())
		return
	}
	item.Candidates, err = json.Marshal(result.Candidates)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "match_state_failed", err.Error())
		return
	}
	item.SelectedCandidateID = ""
	item.ReviewFields = nil
	item.ReviewArtwork = false
	item.ReviewArtworkMaxSize = 0
	item.Error = ""
	item.State = "review"
	if len(result.Candidates) == 0 {
		item.State = "no_match"
		item.Error = "重新匹配没有返回候选"
	}
	if err := s.store.UpsertMatchItem(ctx, item); err != nil {
		s.writeError(c, consts.StatusInternalServerError, "match_state_failed", err.Error())
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"data": map[string]any{
		"item": item, "providers": result.Providers,
	}})
}

func (s *Server) handleMatchReviewUpdate(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil || s.store == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "job_unavailable", "审核状态服务尚未启用")
		return
	}
	job, err := s.jobs.Get(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "匹配任务不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	if job.Kind != domain.JobMatch || (job.State != domain.JobReview && job.State != domain.JobPartial) {
		s.writeError(c, consts.StatusConflict, "job_not_reviewable", "匹配任务当前不允许修改审核状态")
		return
	}
	var request matchReviewUpdateRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	if request.State != "review" && request.State != "accepted" && request.State != "skipped" {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "state 必须是 review、accepted 或 skipped")
		return
	}
	item, err := s.store.MatchItem(ctx, job.ID, c.Param("trackId"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "match_item_not_found", "审核曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "match_state_failed", err.Error())
		return
	}
	if request.State == "accepted" && request.SelectedCandidateID == "" {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "接受候选时必须提供 candidateId")
		return
	}
	if request.SelectedCandidateID != "" {
		var candidates []providers.MatchCandidate
		if err := json.Unmarshal(item.Candidates, &candidates); err != nil {
			s.writeError(c, consts.StatusInternalServerError, "match_state_failed", "候选数据损坏")
			return
		}
		found := false
		for _, candidate := range candidates {
			if candidate.ID == request.SelectedCandidateID {
				found = true
				break
			}
		}
		if !found {
			s.writeError(c, consts.StatusBadRequest, "invalid_request", "candidateId 不属于该曲目的候选")
			return
		}
	}
	item.State = request.State
	if request.SelectedCandidateID != "" {
		item.SelectedCandidateID = request.SelectedCandidateID
	}
	if request.Fields != nil {
		item.ReviewFields = append([]string(nil), request.Fields...)
	}
	if request.Artwork != nil {
		item.ReviewArtwork = *request.Artwork
	}
	if request.ArtworkMaxSize != nil {
		if err := validateArtworkMaxSize(*request.ArtworkMaxSize); err != nil {
			s.writeError(c, consts.StatusBadRequest, "invalid_artwork_resize", err.Error())
			return
		}
		item.ReviewArtworkMaxSize = *request.ArtworkMaxSize
	}
	if err := s.store.UpsertMatchItem(ctx, item); err != nil {
		s.writeError(c, consts.StatusInternalServerError, "match_state_failed", err.Error())
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"data": item})
}

func (s *Server) handleBatchEditItems(ctx context.Context, c *app.RequestContext) {
	if s.store == nil {
		s.writeData(c, []store.BatchEditItem{})
		return
	}
	items, err := s.store.ListBatchEditItems(ctx, c.Param("id"))
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "batch_edit_items_failed", err.Error())
		return
	}
	s.writeData(c, items)
}

type writeSelection struct {
	TrackID        string   `json:"trackId"`
	CandidateID    string   `json:"candidateId"`
	BaseRevision   string   `json:"baseRevision"`
	Fields         []string `json:"fields"`
	Artwork        bool     `json:"artwork,omitempty"`
	ArtworkMaxSize int      `json:"artworkMaxSize,omitempty"`
}

type matchWriteRequest struct {
	Items []writeSelection `json:"items"`
}

func (s *Server) handleMatchWrite(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil || s.store == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "job_unavailable", "写入任务队列尚未启用")
		return
	}
	matchJob, err := s.jobs.Get(ctx, c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(c, consts.StatusNotFound, "job_not_found", "匹配任务不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "jobs_failed", err.Error())
		return
	}
	if matchJob.Kind != domain.JobMatch || (matchJob.State != domain.JobReview && matchJob.State != domain.JobPartial) {
		s.writeError(c, consts.StatusConflict, "job_not_reviewable", "匹配任务尚未进入审核状态")
		return
	}
	var request matchWriteRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil || len(request.Items) == 0 {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "items 不能为空且请求 JSON 必须有效")
		return
	}
	for _, selection := range request.Items {
		if err := validateArtworkMaxSize(selection.ArtworkMaxSize); err != nil {
			s.writeError(c, consts.StatusBadRequest, "invalid_artwork_resize", err.Error())
			return
		}
		if !selection.Artwork && selection.ArtworkMaxSize != 0 {
			s.writeError(c, consts.StatusBadRequest, "invalid_artwork_resize", "未选择写入封面时不能设置封面尺寸")
			return
		}
	}
	payload, _ := json.Marshal(struct {
		MatchJobID string           `json:"matchJobId"`
		Items      []writeSelection `json:"items"`
	}{matchJob.ID, request.Items})
	job, err := s.jobs.Enqueue(ctx, domain.Job{Kind: domain.JobWrite, LibraryID: s.library.Library().ID, Title: "批量安全写入标签", Detail: "等待写入 worker", Total: len(request.Items), Payload: string(payload)})
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "job_enqueue_failed", err.Error())
		return
	}
	for _, selection := range request.Items {
		item, itemErr := s.store.MatchItem(ctx, matchJob.ID, selection.TrackID)
		if itemErr != nil {
			s.writeError(c, consts.StatusInternalServerError, "match_state_update_failed", itemErr.Error())
			return
		}
		item.State = "write_pending"
		item.SelectedCandidateID = selection.CandidateID
		item.ReviewFields = append([]string(nil), selection.Fields...)
		item.ReviewArtwork = selection.Artwork
		item.ReviewArtworkMaxSize = selection.ArtworkMaxSize
		if itemErr := s.store.UpsertMatchItem(ctx, item); itemErr != nil {
			s.writeError(c, consts.StatusInternalServerError, "match_state_update_failed", itemErr.Error())
			return
		}
	}
	c.JSON(consts.StatusAccepted, map[string]any{"data": toJobResponse(job)})
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

func (s *Server) handleTrackScan(ctx context.Context, c *app.RequestContext) {
	track, err := s.library.RescanTrack(ctx, c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusUnprocessableEntity, "track_scan_failed", err.Error())
		return
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, track)
}

func (s *Server) handleBatchEdit(ctx context.Context, c *app.RequestContext) {
	if s.jobs == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "job_unavailable", "任务队列尚未启用")
		return
	}
	var payload domain.BatchEditPayload
	if err := json.Unmarshal(c.Request.Body(), &payload); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	if err := s.validateBatchEditPayload(&payload); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	job, err := s.jobs.Enqueue(ctx, domain.Job{Kind: domain.JobBatchEdit, LibraryID: s.library.Library().ID, Title: "批量编辑标签", Detail: "等待批量编辑 worker", Total: len(payload.Items), Payload: mustJSON(payload)})
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "job_enqueue_failed", err.Error())
		return
	}
	c.JSON(consts.StatusAccepted, map[string]any{"data": toJobResponse(job)})
}

func (s *Server) validateBatchEditPayload(payload *domain.BatchEditPayload) error {
	if len(payload.Items) == 0 || len(payload.Items) > 1000 {
		return fmt.Errorf("items 必须在 1 到 1000 之间")
	}
	if len(payload.Operations) == 0 && !payload.SequenceTracks && payload.Artwork == nil {
		return fmt.Errorf("至少选择一个字段操作、音轨序号操作或封面操作")
	}
	if payload.Artwork != nil {
		if payload.Artwork.Action != domain.BatchArtworkReplace && payload.Artwork.Action != domain.BatchArtworkDelete {
			return fmt.Errorf("不支持的批量封面操作：%s", payload.Artwork.Action)
		}
		if err := validateArtworkMaxSize(payload.Artwork.MaxSize); err != nil {
			return err
		}
		if payload.Artwork.Action == domain.BatchArtworkReplace {
			if payload.Artwork.Data == "" || strings.TrimSpace(payload.Artwork.MIME) == "" {
				return fmt.Errorf("替换封面必须提供图片数据和 MIME")
			}
			data, err := base64.StdEncoding.DecodeString(payload.Artwork.Data)
			if err != nil {
				return fmt.Errorf("批量封面数据不是有效 base64")
			}
			if _, err := artwork.Validate(data, payload.Artwork.MIME); err != nil {
				return fmt.Errorf("批量封面校验失败：%w", err)
			}
		}
	}
	allowed := map[string]bool{
		"title": true, "artists": true, "album": true, "albumArtists": true, "year": true, "genres": true,
		"comment": true, "composers": true, "conductor": true, "lyricists": true,
		"copyright": true, "bpm": true, "isrc": true,
	}
	noAppend := map[string]bool{"title": true, "album": true, "year": true, "comment": true, "conductor": true, "copyright": true, "bpm": true, "isrc": true}
	noReplace := map[string]bool{"year": true, "bpm": true}
	seenOperations := make(map[string]struct{}, len(payload.Operations))
	for _, operation := range payload.Operations {
		if !allowed[operation.Field] {
			return fmt.Errorf("不支持的批量字段：%s", operation.Field)
		}
		if operation.Mode != domain.BatchEditSet && operation.Mode != domain.BatchEditAppend && operation.Mode != domain.BatchEditDelete && operation.Mode != domain.BatchEditReplace {
			return fmt.Errorf("不支持的批量操作：%s", operation.Mode)
		}
		if noAppend[operation.Field] && operation.Mode == domain.BatchEditAppend {
			return fmt.Errorf("字段 %s 不支持追加操作", operation.Field)
		}
		if noReplace[operation.Field] && operation.Mode == domain.BatchEditReplace {
			return fmt.Errorf("字段 %s 不支持查找替换", operation.Field)
		}
		if operation.Mode == domain.BatchEditReplace && strings.TrimSpace(operation.Find) == "" {
			return fmt.Errorf("字段 %s 的查找内容不能为空", operation.Field)
		}
		if operation.Mode != domain.BatchEditDelete && operation.Mode != domain.BatchEditReplace && strings.TrimSpace(operation.Value) == "" {
			return fmt.Errorf("字段 %s 的设置值不能为空", operation.Field)
		}
		if operation.Field == "year" && operation.Mode != domain.BatchEditDelete {
			year, err := strconv.Atoi(strings.TrimSpace(operation.Value))
			if err != nil || year <= 0 {
				return fmt.Errorf("年份必须是正整数")
			}
		}
		if operation.Field == "bpm" && operation.Mode != domain.BatchEditDelete {
			bpm, err := strconv.Atoi(strings.TrimSpace(operation.Value))
			if err != nil || bpm < 1 || bpm > 1000 {
				return fmt.Errorf("BPM 必须是 1 到 1000 的整数")
			}
		}
		if _, exists := seenOperations[operation.Field]; exists {
			return fmt.Errorf("字段 %s 重复操作", operation.Field)
		}
		seenOperations[operation.Field] = struct{}{}
	}
	seenTracks := make(map[string]struct{}, len(payload.Items))
	for index := range payload.Items {
		item := &payload.Items[index]
		if item.TrackID == "" {
			return fmt.Errorf("trackId 不能为空")
		}
		if _, exists := seenTracks[item.TrackID]; exists {
			return fmt.Errorf("trackId 重复：%s", item.TrackID)
		}
		seenTracks[item.TrackID] = struct{}{}
		track, err := s.library.Track(item.TrackID)
		if errors.Is(err, library.ErrTrackNotFound) {
			return fmt.Errorf("曲目不存在：%s", item.TrackID)
		}
		if err != nil {
			return err
		}
		if item.BaseRevision == "" {
			item.BaseRevision = track.Revision
		}
	}
	return nil
}

func mustJSON(value any) string {
	payload, _ := json.Marshal(value)
	return string(payload)
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

func (s *Server) handleRawTags(ctx context.Context, c *app.RequestContext) {
	if s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "raw_tags_unavailable", "原始标签读取服务尚未启用")
		return
	}
	track, err := s.library.Track(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ref, err := s.library.FileRef(track.ID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	raw, err := s.writer.ReadRawTags(ctx, ref)
	if errors.Is(err, filewrite.ErrPathOutsideRoot) {
		s.writeError(c, consts.StatusForbidden, "forbidden", "文件路径不在曲库安全边界内")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		s.writeError(c, consts.StatusNotFound, "track_file_not_found", "音频文件不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusUnprocessableEntity, "tag_read_failed", err.Error())
		return
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, map[string]any{"trackId": track.ID, "revision": track.Revision, "tags": raw})
}

func (s *Server) handleLyricsSidecarGet(ctx context.Context, c *app.RequestContext) {
	if s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "sidecar_unavailable", "歌词 sidecar 服务尚未启用")
		return
	}
	track, err := s.library.Track(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ref, err := s.library.FileRef(track.ID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	snapshot, err := s.writer.ReadSidecar(ctx, ref)
	if err != nil {
		s.handleSidecarError(c, err)
		return
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, map[string]any{"trackId": track.ID, "revision": track.Revision, "sidecar": snapshot.Info, "content": snapshot.Content})
}

type lyricsSidecarRequest struct {
	BaseRevision        string `json:"baseRevision"`
	BaseSidecarRevision string `json:"baseSidecarRevision"`
	Content             string `json:"content"`
	DryRun              bool   `json:"dryRun"`
}

func (s *Server) handleLyricsSidecarPut(ctx context.Context, c *app.RequestContext) {
	var request lyricsSidecarRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	s.handleLyricsSidecarMutation(ctx, c, &request, &request.Content)
}

func (s *Server) handleLyricsSidecarDelete(ctx context.Context, c *app.RequestContext) {
	var request lyricsSidecarRequest
	if len(c.Request.Body()) > 0 {
		if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
			s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
			return
		}
	}
	s.handleLyricsSidecarMutation(ctx, c, &request, nil)
}

func (s *Server) handleLyricsSidecarMutation(ctx context.Context, c *app.RequestContext, request *lyricsSidecarRequest, content *string) {
	if s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "sidecar_unavailable", "歌词 sidecar 服务尚未启用")
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
	track, err := s.library.Track(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ref, err := s.library.FileRef(track.ID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	result, err := s.writer.WriteSidecar(ctx, ref, request.BaseRevision, request.BaseSidecarRevision, content, request.DryRun)
	if err != nil {
		s.handleWriteError(c, err)
		return
	}
	if request.DryRun {
		s.writeData(c, map[string]any{"preview": result})
		return
	}
	if result.Changed {
		if _, err := s.library.RescanTrack(ctx, track.ID); err != nil {
			s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "sidecar 已写入，但重新索引失败："+err.Error())
			return
		}
	}
	updated, err := s.library.Track(track.ID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "reindex_failed", "sidecar 已写入，但曲目索引不可用")
		return
	}
	if result.Changed && s.store != nil {
		_, historyErr := s.store.CreateRevision(ctx, domain.Revision{
			LibraryID: s.library.Library().ID, TrackID: updated.ID, TrackTitle: updated.Title, FileName: updated.FileName,
			Action: sidecarAction(sidecarOperation(result)), Source: "手工编辑", BaseRevision: result.BaseRevision,
			ResultRevision: updated.Revision, Diff: []domain.RevisionDiff{sidecarDiff(result)}, CoverTone: updated.CoverTone,
			BeforeSidecar: sidecarAuditSnapshot(result.Before, result.BeforeContent),
			AfterSidecar:  sidecarAuditSnapshot(result.After, result.AfterContent),
		})
		if historyErr != nil {
			result.Warnings = append(result.Warnings, "sidecar 修订历史写入失败："+historyErr.Error())
		}
	}
	s.writeData(c, map[string]any{"track": updated, "sidecar": result})
}

func sidecarAction(operation domain.Operation) string {
	if operation == domain.OperationDelete {
		return "删除歌词 sidecar"
	}
	return "写入歌词 sidecar"
}

func sidecarOperation(result filewrite.SidecarResult) domain.Operation {
	if result.After != nil && result.After.Exists {
		return domain.OperationSet
	}
	return domain.OperationDelete
}

func sidecarDiff(result filewrite.SidecarResult) domain.RevisionDiff {
	return domain.RevisionDiff{
		Field: "lyricsSidecar", Operation: sidecarOperation(result),
		Before: sidecarAuditValue(result.Before), After: sidecarAuditValue(result.After),
	}
}

func sidecarAuditValue(info *domain.SidecarInfo) any {
	if info == nil || !info.Exists {
		return nil
	}
	return map[string]any{
		"exists":     info.Exists,
		"revision":   info.Revision,
		"sizeBytes":  info.SizeBytes,
		"modifiedAt": info.ModifiedAt,
	}
}

func sidecarAuditSnapshot(info *domain.SidecarInfo, content string) *domain.SidecarSnapshot {
	if info == nil || !info.Exists {
		return nil
	}
	return &domain.SidecarSnapshot{
		Exists: info.Exists, Revision: info.Revision, SizeBytes: info.SizeBytes,
		ModifiedAt: info.ModifiedAt, Content: content,
	}
}

func (s *Server) handleSidecarError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, filewrite.ErrSidecarTooLarge):
		s.writeError(c, consts.StatusRequestEntityTooLarge, "sidecar_too_large", "歌词 sidecar 超过 1 MiB 限制")
	case errors.Is(err, filewrite.ErrPathOutsideRoot):
		s.writeError(c, consts.StatusForbidden, "forbidden", "文件路径不在曲库安全边界内")
	default:
		s.writeError(c, consts.StatusInternalServerError, "sidecar_read_failed", err.Error())
	}
}

func (s *Server) handleAudio(_ context.Context, c *app.RequestContext) {
	if s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "audio_unavailable", "音频读取服务尚未启用")
		return
	}
	track, err := s.library.Track(c.Param("id"))
	if errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ref, err := s.library.FileRef(track.ID)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	file, err := s.writer.OpenRead(ref)
	if errors.Is(err, filewrite.ErrPathOutsideRoot) {
		s.writeError(c, consts.StatusForbidden, "forbidden", "文件路径不在曲库安全边界内")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		s.writeError(c, consts.StatusNotFound, "audio_not_found", "音频文件不存在")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "audio_open_failed", err.Error())
		return
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		s.writeError(c, consts.StatusInternalServerError, "audio_stat_failed", err.Error())
		return
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		s.writeError(c, consts.StatusNotFound, "audio_not_found", "音频文件不存在")
		return
	}

	etag := `"` + track.Revision + `"`
	c.Header("ETag", etag)
	c.Header("Accept-Ranges", "bytes")
	c.SetContentType(audioContentType(track.Format))
	if audioETagMatches(c.Request.Header.Peek("If-None-Match"), etag) {
		_ = file.Close()
		c.SetStatusCode(consts.StatusNotModified)
		return
	}

	size := info.Size()
	start, end := int64(0), size-1
	status := consts.StatusOK
	if rawRange := c.Request.Header.PeekRange(); len(rawRange) > 0 {
		maxInt := int64(^uint(0) >> 1)
		if size > maxInt || size == 0 {
			_ = file.Close()
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			c.SetStatusCode(consts.StatusRequestedRangeNotSatisfiable)
			return
		}
		startPos, endPos, rangeErr := app.ParseByteRange(rawRange, int(size))
		if rangeErr != nil {
			_ = file.Close()
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			c.SetStatusCode(consts.StatusRequestedRangeNotSatisfiable)
			return
		}
		start, end = int64(startPos), int64(endPos)
		if _, seekErr := file.Seek(start, io.SeekStart); seekErr != nil {
			_ = file.Close()
			s.writeError(c, consts.StatusInternalServerError, "audio_seek_failed", seekErr.Error())
			return
		}
		status = consts.StatusPartialContent
		c.Response.Header.SetContentRange(startPos, endPos, int(size))
	}

	length := end - start + 1
	if length < 0 {
		_ = file.Close()
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
		c.SetStatusCode(consts.StatusRequestedRangeNotSatisfiable)
		return
	}
	c.SetStatusCode(status)
	c.SetBodyStream(&closeOnRead{Reader: io.LimitReader(file, length), Closer: file}, int(length))
}

func audioETagMatches(header []byte, etag string) bool {
	for _, candidate := range strings.Split(string(header), ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || candidate == "W/"+etag {
			return true
		}
	}
	return false
}

func audioContentType(format domain.TrackFormat) string {
	switch format {
	case domain.FormatMP3:
		return "audio/mpeg"
	case domain.FormatFLAC:
		return "audio/flac"
	case domain.FormatWAV:
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}

type closeOnRead struct {
	io.Reader
	io.Closer
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
	if request.DryRun {
		s.writeData(c, map[string]any{"preview": result})
		return
	}
	if !result.Changed {
		track, trackErr := s.library.Track(ref.ID)
		if trackErr != nil {
			s.writeError(c, consts.StatusInternalServerError, "internal_error", trackErr.Error())
			return
		}
		c.Header("ETag", `"`+track.Revision+`"`)
		s.writeData(c, map[string]any{"track": track, "write": result})
		return
	}
	if _, err := s.library.RescanTrack(ctx, ref.ID); err != nil {
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
	BeforeSidecar   *sidecarResponse      `json:"beforeSidecar,omitempty"`
	AfterSidecar    *sidecarResponse      `json:"afterSidecar,omitempty"`
}

type sidecarResponse struct {
	Exists     bool   `json:"exists"`
	Revision   string `json:"revision,omitempty"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
}

func (s *Server) handleRevisions(ctx context.Context, c *app.RequestContext) {
	if s.store == nil {
		s.writeData(c, []revisionResponse{})
		return
	}
	limit := 100
	if raw := strings.TrimSpace(string(c.Query("limit"))); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 100 {
		limit = 100
	}
	revisions, err := s.store.ListRevisions(ctx, limit)
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
		BeforeSidecar: toSidecarResponse(revision.BeforeSidecar), AfterSidecar: toSidecarResponse(revision.AfterSidecar),
	}
}

func toSidecarResponse(snapshot *domain.SidecarSnapshot) *sidecarResponse {
	if snapshot == nil {
		return nil
	}
	return &sidecarResponse{Exists: snapshot.Exists, Revision: snapshot.Revision, SizeBytes: snapshot.SizeBytes, ModifiedAt: snapshot.ModifiedAt}
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
	var result filewrite.Result
	if hasRestorableTagFields(revision.Fields) {
		targetTags := revision.BeforeTags
		if request.Target == "after" {
			targetTags = revision.AfterTags
		}
		result, err = s.writer.Restore(ctx, ref, request.BaseRevision, targetTags, dryRun)
		if err != nil {
			s.handleWriteError(c, err)
			return
		}
	} else {
		// Artwork-only and sidecar-only revisions still need the audio revision
		// guard, but must not interpret an empty tag snapshot as “delete every
		// tag”. The subsequent asset writer performs the real current-file check.
		result = filewrite.Result{BaseRevision: request.BaseRevision, CurrentRevision: request.BaseRevision, DryRun: dryRun}
	}
	var artworkResult *filewrite.ArtworkResult
	if targetArtwork, hasArtwork := revisionArtworkTarget(revision, request.Target); hasArtwork {
		artResult, artworkErr := s.writer.WriteArtwork(ctx, ref, result.CurrentRevision, 0, targetArtwork, dryRun)
		if artworkErr != nil {
			s.handleWriteError(c, artworkErr)
			return
		}
		artworkResult = &artResult
		result.Changed = result.Changed || artResult.Changed
		result.CurrentRevision = artResult.CurrentRevision
		result.Diff = append(result.Diff, artResult.Diff...)
		result.Warnings = append(result.Warnings, artResult.Warnings...)
		result.AfterTags = artResult.AfterTags
	}
	var sidecarResult *filewrite.SidecarResult
	if targetSidecar, hasSidecar := revisionSidecarTarget(revision, request.Target); hasSidecar {
		currentSidecar, sidecarErr := s.writer.ReadSidecar(ctx, ref)
		if sidecarErr != nil {
			s.handleSidecarError(c, sidecarErr)
			return
		}
		baseSidecarRevision := ""
		if currentSidecar.Info != nil {
			baseSidecarRevision = currentSidecar.Info.Revision
		}
		var content *string
		if targetSidecar != nil && targetSidecar.Exists {
			value := targetSidecar.Content
			content = &value
		}
		written, sidecarErr := s.writer.WriteSidecar(ctx, ref, result.CurrentRevision, baseSidecarRevision, content, dryRun)
		if sidecarErr != nil {
			s.handleWriteError(c, sidecarErr)
			return
		}
		sidecarResult = &written
		result.Sidecar = sidecarResult
		result.Changed = result.Changed || written.Changed
		if written.Changed {
			result.Diff = append(result.Diff, sidecarDiff(written))
		}
		result.Warnings = append(result.Warnings, written.Warnings...)
	}
	if dryRun {
		s.writeData(c, map[string]any{
			"revisionId": revision.ID, "trackId": revision.TrackID, "target": request.Target, "preview": result,
		})
		return
	}
	if result.Changed {
		if _, err := s.library.RescanTrack(ctx, revision.TrackID); err != nil {
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
			BeforeArtwork: artworkResultSnapshot(artworkResult, true), AfterArtwork: artworkResultSnapshot(artworkResult, false),
			BeforeSidecar: sidecarResultSnapshot(sidecarResult, true), AfterSidecar: sidecarResultSnapshot(sidecarResult, false),
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

func revisionArtworkTarget(revision domain.Revision, target string) (*artwork.Asset, bool) {
	if !slices.Contains(revision.Fields, "artwork") {
		return nil, false
	}
	snapshot := revision.BeforeArtwork
	if target == "after" {
		snapshot = revision.AfterArtwork
	}
	if snapshot == nil {
		return nil, true
	}
	return &artwork.Asset{Data: append([]byte(nil), snapshot.Data...), MIME: snapshot.MIME, Format: snapshot.Format, Width: snapshot.Width, Height: snapshot.Height, Size: snapshot.Size, Hash: snapshot.Hash}, true
}

func hasRestorableTagFields(fields []string) bool {
	for _, field := range fields {
		switch field {
		case "title", "artists", "album", "albumArtists", "trackNumber", "trackTotal", "discNumber", "discTotal", "year", "genres", "lyrics":
			return true
		}
	}
	return false
}

func revisionSidecarTarget(revision domain.Revision, target string) (*domain.SidecarSnapshot, bool) {
	if !slices.Contains(revision.Fields, "lyricsSidecar") {
		return nil, false
	}
	if target == "after" {
		return revision.AfterSidecar, true
	}
	return revision.BeforeSidecar, true
}

func sidecarResultSnapshot(result *filewrite.SidecarResult, before bool) *domain.SidecarSnapshot {
	if result == nil {
		return nil
	}
	if before {
		return sidecarAuditSnapshot(result.Before, result.BeforeContent)
	}
	return sidecarAuditSnapshot(result.After, result.AfterContent)
}

func artworkResultSnapshot(result *filewrite.ArtworkResult, before bool) *domain.ArtworkSnapshot {
	if result == nil {
		return nil
	}
	if before {
		return artworkSnapshot(result.Before)
	}
	return artworkSnapshot(result.After)
}

func (s *Server) handleWriteError(c *app.RequestContext, err error) {
	var conflict *filewrite.RevisionConflictError
	var sidecarConflict *filewrite.SidecarConflictError
	switch {
	case errors.As(err, &conflict):
		c.JSON(consts.StatusConflict, map[string]any{
			"error": map[string]any{
				"code":    "revision_conflict",
				"message": "文件已被其他操作修改",
				"details": map[string]string{"current_revision": conflict.Current},
			},
		})
	case errors.As(err, &sidecarConflict):
		c.JSON(consts.StatusConflict, map[string]any{
			"error": map[string]any{
				"code":    "sidecar_revision_conflict",
				"message": "歌词 sidecar 已被其他操作修改",
				"details": map[string]string{"current_sidecar_revision": sidecarConflict.Current},
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
	case errors.Is(err, filewrite.ErrSidecarTooLarge):
		s.writeError(c, consts.StatusRequestEntityTooLarge, "sidecar_too_large", "歌词 sidecar 超过 1 MiB 限制")
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
	if maxSize, resizeErr := artworkResizeQuery(c); resizeErr != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_artwork_resize", resizeErr.Error())
		return
	} else if maxSize > 0 {
		asset, err = artwork.ResizeSquare(asset, maxSize)
		if err != nil {
			s.writeError(c, consts.StatusUnprocessableEntity, "invalid_artwork_resize", err.Error())
			return
		}
	}
	s.handleArtworkMutation(ctx, c, &asset)
}

func artworkResizeQuery(c *app.RequestContext) (int, error) {
	value := strings.TrimSpace(string(c.Query("max_size")))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("max_size must be 500 or 1000")
	}
	if err := validateArtworkMaxSize(parsed); err != nil {
		return 0, err
	}
	return parsed, nil
}

func validateArtworkMaxSize(value int) error {
	if value != 0 && value != 500 && value != 1000 {
		return fmt.Errorf("封面尺寸必须为原图、500 或 1000")
	}
	return nil
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
	action, source := "替换封面", "手工上传"
	if target == nil {
		action, source = "删除封面", "手工操作"
	}
	s.finishArtworkMutation(ctx, c, ref, result, action, source)
}

func (s *Server) finishArtworkMutation(ctx context.Context, c *app.RequestContext, ref library.FileRef, result filewrite.ArtworkResult, action, source string) {
	if result.Changed {
		if _, err := s.library.RescanTrack(ctx, ref.ID); err != nil {
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
		_, historyErr := s.store.CreateRevision(ctx, domain.Revision{
			LibraryID: s.library.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName,
			Action: action, Source: source, BaseRevision: result.BaseRevision, ResultRevision: track.Revision,
			Diff: result.Diff, CoverTone: track.CoverTone, BeforeTags: result.BeforeTags, AfterTags: result.AfterTags,
			BeforeArtwork: artworkSnapshot(result.Before), AfterArtwork: artworkSnapshot(result.After),
		})
		if historyErr != nil {
			result.Warnings = append(result.Warnings, "修订历史写入失败："+historyErr.Error())
		}
	}
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, map[string]any{"track": track, "write": result})
}

func artworkSnapshot(asset *artwork.Asset) *domain.ArtworkSnapshot {
	if asset == nil {
		return nil
	}
	return &domain.ArtworkSnapshot{
		MIME: asset.MIME, Format: asset.Format, Width: asset.Width, Height: asset.Height,
		Size: asset.Size, Hash: asset.Hash, Data: append([]byte(nil), asset.Data...),
	}
}

type matchArtworkRequest struct {
	CandidateID  string `json:"candidateId"`
	BaseRevision string `json:"baseRevision"`
	MaxSize      int    `json:"maxSize"`
	DryRun       bool   `json:"dryRun"`
}

func (s *Server) handleMatchArtwork(ctx context.Context, c *app.RequestContext) {
	if s.providers == nil || s.writer == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "provider_unavailable", "抓取器或封面写入服务尚未初始化")
		return
	}
	var request matchArtworkRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	headerRevision := strings.Trim(strings.TrimSpace(string(c.Request.Header.Peek("If-Match"))), `"`)
	if request.BaseRevision == "" {
		request.BaseRevision = headerRevision
	}
	if request.CandidateID == "" || request.BaseRevision == "" || (headerRevision != "" && headerRevision != request.BaseRevision) {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "candidateId、baseRevision 与一致的 If-Match 不能为空")
		return
	}
	if request.MaxSize != 0 && request.MaxSize != 500 && request.MaxSize != 1000 {
		s.writeError(c, consts.StatusBadRequest, "invalid_artwork_resize", "maxSize 必须为 500 或 1000")
		return
	}
	reference, err := s.providers.ArtworkReference(request.CandidateID)
	if errors.Is(err, providers.ErrArtworkReferenceNotFound) {
		s.writeError(c, consts.StatusNotFound, "candidate_artwork_expired", "候选封面不存在或已过期，请重新搜索")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	descriptor, found := s.providers.Descriptor(reference.ProviderID)
	if !found {
		s.writeError(c, consts.StatusNotFound, "provider_not_found", "数据来源不存在")
		return
	}
	asset, err := s.downloadArtwork(ctx, reference)
	if errors.Is(err, providers.ErrUnsafeArtworkURL) || errors.Is(err, artwork.ErrInvalid) {
		s.writeError(c, consts.StatusUnprocessableEntity, "invalid_provider_artwork", err.Error())
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusBadGateway, "provider_artwork_failed", err.Error())
		return
	}
	if request.MaxSize > 0 {
		asset, err = artwork.ResizeSquare(asset, request.MaxSize)
		if err != nil {
			s.writeError(c, consts.StatusUnprocessableEntity, "invalid_artwork_resize", err.Error())
			return
		}
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
	result, err := s.writer.WriteArtwork(ctx, ref, request.BaseRevision, 0, &asset, request.DryRun)
	if err != nil {
		s.handleWriteError(c, err)
		return
	}
	if request.DryRun {
		s.writeData(c, map[string]any{"preview": result, "candidateId": request.CandidateID})
		return
	}
	s.finishArtworkMutation(ctx, c, ref, result, "采用数据源封面", descriptor.Name)
}

func (s *Server) handleCandidateArtwork(ctx context.Context, c *app.RequestContext) {
	if s.providers == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "provider_unavailable", "抓取器尚未初始化")
		return
	}
	reference, err := s.providers.ArtworkReference(c.Param("id"))
	if errors.Is(err, providers.ErrArtworkReferenceNotFound) {
		s.writeError(c, consts.StatusNotFound, "candidate_artwork_expired", "候选封面不存在或已过期，请重新搜索")
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	asset, err := s.downloadArtwork(ctx, reference)
	if errors.Is(err, providers.ErrUnsafeArtworkURL) || errors.Is(err, artwork.ErrInvalid) {
		s.writeError(c, consts.StatusUnprocessableEntity, "invalid_provider_artwork", err.Error())
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusBadGateway, "provider_artwork_failed", err.Error())
		return
	}
	c.SetContentType(asset.MIME)
	c.Header("Cache-Control", "private, max-age=300")
	c.Response.SetBody(asset.Data)
}

func (s *Server) handleProviders(_ context.Context, c *app.RequestContext) {
	if s.providers == nil {
		s.writeData(c, []providers.Descriptor{})
		return
	}
	s.writeData(c, s.providers.Descriptors())
}

type providerUpdateRequest struct {
	Enabled *bool             `json:"enabled,omitempty"`
	Config  map[string]string `json:"config,omitempty"`
}

func (s *Server) handleProviderUpdate(ctx context.Context, c *app.RequestContext) {
	if s.providers == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "provider_unavailable", "抓取器尚未初始化")
		return
	}
	var request providerUpdateRequest
	if err := json.Unmarshal(c.Request.Body(), &request); err != nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "请求 JSON 无效")
		return
	}
	if request.Enabled == nil && request.Config == nil {
		s.writeError(c, consts.StatusBadRequest, "invalid_request", "至少提供 enabled 或 config")
		return
	}
	var descriptor providers.Descriptor
	var err error
	if request.Enabled != nil {
		descriptor, err = s.providers.SetEnabled(ctx, c.Param("id"), *request.Enabled)
	}
	if err == nil && request.Config != nil {
		descriptor, err = s.providers.SetConfig(ctx, c.Param("id"), request.Config)
	}
	if errors.Is(err, providers.ErrProviderNotFound) {
		s.writeError(c, consts.StatusNotFound, "provider_not_found", err.Error())
		return
	}
	if errors.Is(err, providers.ErrProviderUnavailable) {
		s.writeError(c, consts.StatusUnprocessableEntity, "provider_unavailable", err.Error())
		return
	}
	if errors.Is(err, providers.ErrProviderNotConfigurable) || errors.Is(err, providers.ErrProviderConfigInvalid) {
		s.writeError(c, consts.StatusUnprocessableEntity, "provider_config_invalid", err.Error())
		return
	}
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "provider_settings_failed", err.Error())
		return
	}
	s.writeData(c, descriptor)
}

func (s *Server) handleProviderTest(ctx context.Context, c *app.RequestContext) {
	if s.providers == nil {
		s.writeError(c, consts.StatusServiceUnavailable, "provider_unavailable", "抓取器尚未初始化")
		return
	}
	descriptor, found := s.providers.Descriptor(c.Param("id"))
	if !found {
		s.writeError(c, consts.StatusNotFound, "provider_not_found", "数据来源不存在")
		return
	}
	if !descriptor.Enabled {
		s.writeError(c, consts.StatusUnprocessableEntity, "provider_disabled", "请先启用数据来源")
		return
	}
	query := providers.Query{Title: "最佳歌手", Artists: []string{"许嵩"}}
	limit := 1
	probeArtwork := false
	logs := []providerTestLog{{Level: "info", Stage: "request", Message: "开始数据源测试"}}
	body := c.Request.Body()
	if len(strings.TrimSpace(string(body))) > 0 {
		var request struct {
			Query struct {
				Title           string   `json:"title"`
				Artists         []string `json:"artists"`
				Album           string   `json:"album"`
				DurationSeconds int64    `json:"durationSeconds"`
			} `json:"query"`
			Limit        int  `json:"limit"`
			ProbeArtwork bool `json:"probeArtwork"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			s.writeError(c, consts.StatusBadRequest, "invalid_request", "测试查询 JSON 无效")
			return
		}
		if strings.TrimSpace(request.Query.Title) == "" {
			s.writeError(c, consts.StatusBadRequest, "invalid_request", "测试查询需要歌曲名")
			return
		}
		query = providers.Query{
			Title: strings.TrimSpace(request.Query.Title), Artists: request.Query.Artists,
			Album: strings.TrimSpace(request.Query.Album), DurationSeconds: request.Query.DurationSeconds,
		}
		if request.Limit > 0 {
			limit = request.Limit
		}
		probeArtwork = request.ProbeArtwork
	}
	if limit > 10 {
		limit = 10
	}
	logs = append(logs, providerTestLog{Level: "info", Stage: "query", Message: "已提交搜索查询", Details: map[string]any{
		"title": query.Title, "artists": query.Artists, "album": query.Album, "durationSeconds": query.DurationSeconds, "limit": limit,
	}})
	result, err := s.providers.Search(ctx, query, []string{descriptor.ID}, limit)
	if err != nil {
		s.writeError(c, consts.StatusBadGateway, "provider_test_failed", err.Error())
		return
	}
	outcome := result.Providers[descriptor.ID]
	if outcome.Status != "ok" {
		s.writeError(c, consts.StatusBadGateway, "provider_test_failed", outcome.Error)
		return
	}
	logs = append(logs, providerTestLog{Level: "success", Stage: "search", Message: "数据源搜索完成", Details: map[string]any{
		"status": outcome.Status, "count": outcome.Count, "latencyMs": outcome.LatencyMS, "cached": outcome.Cached,
	}})
	for _, candidate := range result.Candidates {
		lyricsChars := 0
		if candidate.Lyrics != nil {
			lyricsChars = len([]rune(candidate.Lyrics.Value))
		}
		details := map[string]any{
			"candidateId": candidate.ID, "externalId": candidate.ExternalID, "score": candidate.Score,
			"hasArtwork": candidate.HasArtwork, "hasLyrics": candidate.HasLyrics, "lyricsChars": lyricsChars,
		}
		if reference, referenceErr := s.providers.ArtworkReference(candidate.ID); referenceErr == nil {
			if parsed, parseErr := url.Parse(reference.URL); parseErr == nil {
				details["artworkHost"] = parsed.Hostname()
				details["artworkPath"] = parsed.EscapedPath()
			}
			details["artworkExpiresAt"] = reference.ExpiresAt.Format(time.RFC3339)
		}
		logs = append(logs, providerTestLog{Level: "info", Stage: "candidate", Message: "收到候选：" + candidate.Title.Value, Details: details})
		if !probeArtwork || !candidate.HasArtwork {
			continue
		}
		reference, referenceErr := s.providers.ArtworkReference(candidate.ID)
		if referenceErr != nil {
			logs = append(logs, providerTestLog{Level: "warning", Stage: "artwork", Message: "候选封面引用不存在", Details: map[string]any{"candidateId": candidate.ID}})
			continue
		}
		asset, artworkErr := s.downloadArtwork(ctx, reference)
		if artworkErr != nil {
			logs = append(logs, providerTestLog{Level: "error", Stage: "artwork", Message: "封面探测失败", Details: map[string]any{"candidateId": candidate.ID, "error": artworkErr.Error()}})
			continue
		}
		logs = append(logs, providerTestLog{Level: "success", Stage: "artwork", Message: "封面探测成功", Details: map[string]any{
			"candidateId": candidate.ID, "mime": asset.MIME, "format": asset.Format, "width": asset.Width, "height": asset.Height, "size": asset.Size,
		}})
	}
	s.writeData(c, map[string]any{
		"provider": descriptor,
		"result":   outcome,
		"query": map[string]any{
			"title": query.Title, "artists": query.Artists, "album": query.Album, "durationSeconds": query.DurationSeconds,
		},
		"candidates": result.Candidates,
		"logs":       logs,
	})
}

type providerTestLog struct {
	Level   string         `json:"level"`
	Stage   string         `json:"stage"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
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
	if request.FileID != "" && s.store != nil {
		artists := append([]string(nil), query.Artists...)
		if artists == nil {
			artists = []string{}
		}
		queryPayload, _ := json.Marshal(map[string]any{
			"title": query.Title, "artists": artists, "album": query.Album,
			"durationSeconds": query.DurationSeconds,
		})
		// Search remains successful even if the optional audit trail cannot be persisted.
		_, _ = s.store.AddMatchQueryHistory(ctx, request.FileID, queryPayload, request.ProviderIDs, len(result.Candidates))
	}
	s.writeData(c, result)
}

func (s *Server) handleMatchQueryHistory(ctx context.Context, c *app.RequestContext) {
	if s.store == nil {
		s.writeData(c, []store.MatchQueryHistory{})
		return
	}
	trackID := c.Param("id")
	if _, err := s.library.Track(trackID); errors.Is(err, library.ErrTrackNotFound) {
		s.writeError(c, consts.StatusNotFound, "track_not_found", "曲目不存在")
		return
	} else if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "track_failed", err.Error())
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	history, err := s.store.ListMatchQueryHistory(ctx, trackID, limit)
	if err != nil {
		s.writeError(c, consts.StatusInternalServerError, "match_history_failed", err.Error())
		return
	}
	s.writeData(c, history)
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
