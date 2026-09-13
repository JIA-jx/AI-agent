package main

import (
	"app/internal/inits"
	_ "time/tzdata"

	"thunder/config"
	"thunder/logs"
	"thunder/server"
)

func main() {
	config.Init()
	conf := config.GetConfig()

	logs.Init(conf.Log)

	s := server.NewServer(conf)

	inits.Init(s, conf)
	logs.Info("init finish")

	s.Start()
}
