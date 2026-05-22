// Package api provides the HTTP REST server for the Assiter knowledge graph.
package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/quyenluc/assiter/internal/agent"
	"github.com/quyenluc/assiter/internal/graph"
	"github.com/quyenluc/assiter/internal/ingestion"
	"github.com/quyenluc/assiter/pkg/config"
)

// Server holds all dependencies for the REST API.
type Server struct {
	cfg      *config.Config
	pipeline *ingestion.Pipeline
	graph    *graph.Client
	agent    *agent.Agent
	router   *gin.Engine
}

// New creates and wires a Server.
func New(cfg *config.Config, pipeline *ingestion.Pipeline, g *graph.Client, a *agent.Agent) *Server {
	s := &Server{
		cfg:      cfg,
		pipeline: pipeline,
		graph:    g,
		agent:    a,
		router:   gin.Default(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.router.GET("/health", s.health)
	s.router.POST("/ingest", s.ingest)
	s.router.GET("/graph/search", s.searchGraph)
	s.router.GET("/graph/node/:id", s.getNode)
	s.router.GET("/graph/stats", s.graphStats)
	s.router.POST("/agent/query", s.agentQuery)
}

// Run starts the HTTP server.
func (s *Server) Run() error {
	addr := s.cfg.Server.Host + ":" + itoa(s.cfg.Server.Port)
	return s.router.Run(addr)
}

// --- Handlers ---

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) ingest(c *gin.Context) {
	var req struct {
		Dir     string   `json:"dir" binding:"required"`
		Exclude []string `json:"exclude"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Exclude) == 0 {
		req.Exclude = s.cfg.Parser.Exclude
	}

	result, err := s.pipeline.Run(c.Request.Context(), ingestion.IngestOptions{
		Dir:     req.Dir,
		Exclude: req.Exclude,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) searchGraph(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name query param required"})
		return
	}
	nodes, err := s.graph.SearchNodes(c.Request.Context(), name, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}

func (s *Server) getNode(c *gin.Context) {
	id := c.Param("id")
	result, err := s.graph.GetNodeWithNeighbors(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) graphStats(c *gin.Context) {
	stats, err := s.graph.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (s *Server) agentQuery(c *gin.Context) {
	var req agent.QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}

	resp, err := s.agent.Query(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
