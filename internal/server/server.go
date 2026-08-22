package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"path/filepath"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	hserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/library"
)

type Server struct {
	h             *hserver.Hertz
	library       *library.Service
	writer        *filewrite.Writer
	frontend      fs.FS
	version       string
	tagEngineInfo string
}

func New(listen string, libraryService *library.Service, writer *filewrite.Writer, frontend fs.FS, version, tagEngineInfo string) *Server {
	h := hserver.Default(
		hserver.WithHostPorts(listen),
		hserver.WithMaxRequestBodySize(1<<20),
	)
	s := &Server{
		h:             h,
		library:       libraryService,
		writer:        writer,
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
	c.Header("ETag", `"`+track.Revision+`"`)
	s.writeData(c, map[string]any{"track": track, "write": result})
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
	default:
		s.writeError(c, consts.StatusInternalServerError, "write_failed", err.Error())
	}
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
