package database

import (
	"thunder/config"
	"thunder/db"
)

var (
	RedisCli *db.Redis
)

func InitRedis(conf *config.Redis) {
	if conf == nil {
		return
	}

	r := db.Redis{}
	r.Init(conf)
	RedisCli = &r
}
