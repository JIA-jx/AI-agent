package db

import (
	"basemodel/config"
	"context"
	"github.com/redis/go-redis/v9"
	"time"
)

type Redis struct {
	Options *redis.Options
	Client  *redis.Client
}

func (r *Redis) Init(conf *config.Redis) {
	if r.Options == nil {
		r.Options = &redis.Options{
			Addr:           conf.GetAddr(),
			DB:             conf.GetDB(),
			Password:       conf.GetPassword(),
			PoolSize:       conf.GetPoolSize(),
			MaxIdleConns:   conf.GetMaxIdleConns(),
			MaxActiveConns: conf.GetMaxOpenConns(),
		}
	}
	rdb := redis.NewClient(r.Options)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		panic(err)
	}

	r.Client = rdb
}
