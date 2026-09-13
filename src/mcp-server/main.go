package main

import (
	"mcp-server/internal/inits"

	"thunder/config"
	"thunder/logs"
	"thunder/server"
)

func main() {
	config.Init()
	conf := config.GetConfig()

	logs.Init(conf.Log)
	s := server.NewServer(conf)

	inits.Init(s)

	s.Start()
}
