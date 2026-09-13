package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"thunder/ai/einos"
)

type WeatherTool struct {
	apiKey string
}

type WeatherConfig struct {
	ApiKey string
}

func NewWeatherTool(c *WeatherConfig) einos.InvokeParamTool {
	if c == nil {
		panic("WeatherConfig is nil")
	}

	return &WeatherTool{apiKey: c.ApiKey}
}

func (w *WeatherTool) Params() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"city": {
			Desc:     "需要查询天气的城市名称或区域编码",
			Type:     schema.String,
			Required: true,
		},
		"extensions": {
			Desc: "气象类型：base(实况天气)/all(预报天气)",
			Type: schema.String,
			Enum: []string{"base", "all"},
		},
	}
}

func (w *WeatherTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "get_weather",
		Desc: "查询指定城市的天气信息，使用高德天气API",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"city": {
				Desc:     "需要查询天气的城市名称或区域编码",
				Type:     schema.String,
				Required: true,
			},
			"extensions": {
				Desc: "气象类型：base(实况天气)/all(预报天气)",
				Type: schema.String,
				Enum: []string{"base", "all"},
			},
		}),
	}, nil
}

func (w *WeatherTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var params map[string]any // 解析 JSON 字节流
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", err
	}

	city, ok := params["city"].(string)
	if !ok {
		return "", fmt.Errorf("city is required")
	}

	queryParams := url.Values{}
	queryParams.Set("key", w.apiKey)
	queryParams.Set("city", city)
	if extensions, ok := params["extensions"].(string); ok { // 拓展参数 实况/预报
		queryParams.Set("extensions", extensions)
	} else {
		queryParams.Set("extensions", "base")
	}
	queryParams.Set("output", "JSON")
	baseUrl := "https://restapi.amap.com/v3/weather/weatherInfo"
	fullUrl := fmt.Sprintf("%s?%s", baseUrl, queryParams.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullUrl, nil)
	if err != nil {
		return "", err
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http request failed with status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}
