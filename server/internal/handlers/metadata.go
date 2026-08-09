package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/mediaworkflow"
	"ivideo/server/internal/metadata"
	"ivideo/server/internal/resp"
	"ivideo/server/internal/store"
	"ivideo/server/internal/strm"
)

func (h *Handler) MetadataStatus(c *gin.Context) {
	status, err := h.metadata.Status()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, status)
}

func (h *Handler) SaveMetadataToken(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, http.StatusBadRequest, "请求内容不正确")
		return
	}
	if err := h.metadata.SaveToken(c.Request.Context(), req.Token); err != nil {
		resp.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.OK(c, gin.H{"configured": true})
}

func (h *Handler) ScrapeMetadata(c *gin.Context) {
	err := h.metadata.Start(func(result metadata.Result, err error) {
		h.workflow.CompleteEnrichment("manual metadata", mediaworkflow.EnrichmentResult{ImagePaths: result.ImagePaths}, err)
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, metadata.ErrBusy) {
			status = http.StatusConflict
		}
		resp.Fail(c, status, err.Error())
		return
	}
	status, _ := h.metadata.Status()
	resp.OK(c, status)
}

func (h *Handler) MediaGroups(c *gin.Context) {
	status := c.DefaultQuery("status", "review")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	items, total, err := h.store.ListMediaGroupDetails(status, limit, offset)
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) MediaGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, http.StatusBadRequest, "作品组 ID 不正确")
		return
	}
	detail, err := h.store.GetMediaGroupDetail(id)
	if err != nil {
		resp.Fail(c, http.StatusNotFound, "作品组不存在")
		return
	}
	resp.OK(c, detail)
}

func (h *Handler) ConfirmMediaGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, http.StatusBadRequest, "作品组 ID 不正确")
		return
	}
	var req struct {
		CandidateID int64  `json:"candidateId"`
		Library     string `json:"library"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, http.StatusBadRequest, "请求内容不正确")
		return
	}
	detail, err := h.store.GetMediaGroupDetail(id)
	if err != nil {
		resp.Fail(c, http.StatusNotFound, "作品组不存在")
		return
	}
	var selected *store.MediaCandidate
	for i := range detail.Candidates {
		if detail.Candidates[i].ID == req.CandidateID {
			selected = &detail.Candidates[i]
			break
		}
	}
	if selected == nil {
		resp.Fail(c, http.StatusBadRequest, "请选择有效候选")
		return
	}
	library := req.Library
	if library == "" {
		library = selected.Library
	}
	if !strm.ValidLibrary(library) || library == string(strm.LibReview) {
		resp.Fail(c, http.StatusBadRequest, "请选择正式媒体库")
		return
	}
	decision := store.MediaGroupDecision{
		Status: "verified", DecisionSource: "manual", SelectedSource: selected.Source,
		SelectedID: selected.ProviderID, CanonicalTitle: selected.Title, Library: library,
		Confidence: 100, Reason: "人工按作品组确认",
	}
	if err := h.store.SetMediaGroupDecision(id, decision); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	_ = h.store.UpsertMediaAlias(store.MediaAlias{Alias: detail.Group.RawTitle,
		CanonicalTitle: selected.Title, MediaKind: selected.MediaKind, Source: selected.Source,
		ProviderID: selected.ProviderID, Year: selected.Year, Confidence: 100})
	h.autoScrapeMetadata()
	resp.OK(c, gin.H{"confirmed": true, "members": len(detail.Members)})
}

func (h *Handler) ReviewMediaGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, http.StatusBadRequest, "作品组 ID 不正确")
		return
	}
	_, err = h.store.GetMediaGroupDetail(id)
	if err != nil {
		resp.Fail(c, http.StatusNotFound, "作品组不存在")
		return
	}
	if err := h.store.SetMediaGroupDecision(id, store.MediaGroupDecision{Status: "review", DecisionSource: "manual", Library: string(strm.LibReview), Reason: "人工退回待整理"}); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.autoGenerateStrm("media group review")
	resp.OK(c, gin.H{"review": true})
}

func (h *Handler) SearchMediaGroupCandidates(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, http.StatusBadRequest, "作品组 ID 不正确")
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, http.StatusBadRequest, "请求内容不正确")
		return
	}
	detail, err := h.metadata.SearchCandidates(c.Request.Context(), id, req.Query)
	if err != nil {
		resp.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.OK(c, detail)
}

func (h *Handler) autoScrapeMetadata() {
	h.workflow.StartEnrichment("automatic metadata")
}
