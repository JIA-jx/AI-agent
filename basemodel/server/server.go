package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"basemodel/config"
	"basemodel/event"
	"github.com/gin-gonic/gin"
)

type Server struct {
	Engine     *gin.Engine
	httpServer *http.Server
	conf       *config.Config
	Close      func()
}

func NewServer(conf *config.Config) *Server {
	if conf.Server == nil {
		panic("Server config is required ")
	}
	gin.SetMode(conf.Server.GetMode())

	engine := gin.Default()
	UseMidd(conf, engine)
	return &Server{
		Engine: engine,
		conf:   conf,
	}
}

func (s *Server) RegisterRouters(event event.IEvent, routers ...IRouter) []func() error {
	event.Register()
	var closeFuncs []func() error

	for _, r := range routers {
		r.Register(s.Engine)
		if closer, ok := r.(CloseIRouter); ok {
			closeFuncs = append(closeFuncs, closer.Close)
		}
	}

	log.Println("Routers registered successfully.")
	return closeFuncs
}

func (s *Server) Start() {
	address := fmt.Sprintf("%s:%d", s.conf.Server.GetHost(), s.conf.Server.GetPort())
	readTimeout := s.conf.Server.GetReadTimeout()
	writeTimeout := s.conf.Server.GetWriteTimeout()

	s.httpServer = &http.Server{
		Addr:         address,
		Handler:      s.Engine,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	go func() {
		log.Printf("Server starting on http://%s", address)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit
	log.Println("Shutting down server...")
	if s.Close != nil {
		s.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("Server exited gracefully.")
}
