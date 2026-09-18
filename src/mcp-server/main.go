package main

import (
	"mcp-server/internal/inits"

	"basemodel/config"
	"basemodel/logs"
	"basemodel/server"
)

func main() {
	config.Init()
	conf := config.GetConfig()

	logs.Init(conf.Log)
	s := server.NewServer(conf)

	inits.Init(s)

	s.Start()
}
