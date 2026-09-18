package inits

import (
	"app/internal/router"
	"core/ai"
	"core/ai/tools"

	"basemodel/config"
	"basemodel/database"
	"basemodel/logs"
	"basemodel/server"
	"basemodel/tools/jwt"
)

func Init(s *server.Server, conf *config.Config) {
	database.InitPostgres(conf.DB.Postgres)
	logs.Infof("数据库初始化完成")

	database.InitRedis(conf.DB.Redis)
	logs.Infof("redis初始化完成")

	jwt.Init(conf.Jwt.GetSecret())
	logs.Infof("jwt初始化完成")

	registerTools()
	logs.Infof("系统工具初始化完成")

	ai.Init()
	logs.Infof("工作流执行器初始化完成")
	closeFuncs := s.RegisterRouters(
		&router.Event{},
		&router.HealthRouter{},
		&router.AuthRouter{},
		&router.AgentRouter{},
		&router.LLMRouter{},
		&router.ToolRouter{},
		&router.KnowledgeBaseRouter{},
		&router.A2ARouter{},
		&router.WorkflowRouter{},
		&router.NodeRouter{},
		&router.SkillRouter{},
	)

	eventRouter := &router.Event{}
	eventRouter.Register()
	logs.Infof("路由初始化完成")
	s.Close = func() {
		for _, f := range closeFuncs {
			err := f()
			if err != nil {
				logs.Error("close func error", "error", err)
				return
			}
		}
	}
}

func registerTools() {
	err := tools.InitK8sClient()
	if err != nil {
		logs.Error("init k8s client error", "error", err)
	}

	tools.RegisterSystemTools(
		tools.NewWeatherTool(&tools.WeatherConfig{ApiKey: tools.ApiKey}),
		tools.NewGitTool(), tools.NewGitCommitTool(),
		tools.NewK8sResourceQueryTool(),
		tools.NewK8sLogsTool(),
		tools.NewK8sResourceActionTool(),
		tools.NewK8sHealthCheckTool(),
		tools.NewFileWriteTool(&tools.FileWriteConfig{}),
		tools.NewHTMLToPPTTool(&tools.HTMLToPPTConfig{}),
	)
}
