package database

import (
	"basemodel/config"
	"basemodel/db"
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
