package http

import (
	"github.com/gin-gonic/gin"
)

type Server struct {
	Engine *gin.Engine
}

func NewServer(cfg RouterConfig) *Server {
	return &Server{Engine: NewRouter(cfg)}
}

func (s *Server) Run(address string) error {
	return s.Engine.Run(address)
}
