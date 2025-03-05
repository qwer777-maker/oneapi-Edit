package openai

import (
	"github.com/songquanpeng/one-api/common/env"
	"errors"
	"fmt"
	"log"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/songquanpeng/one-api/relay/adaptor"
	"github.com/songquanpeng/one-api/relay/adaptor/doubao"
	"github.com/songquanpeng/one-api/relay/adaptor/minimax"
	"github.com/songquanpeng/one-api/relay/channeltype"
	"github.com/songquanpeng/one-api/relay/meta"
	"github.com/songquanpeng/one-api/relay/model"
	"github.com/songquanpeng/one-api/relay/relaymode"
	"io"
	"net/http"
	"strings"
)

type Adaptor struct {
	ChannelType int
}

func (a *Adaptor) Init(meta *meta.Meta) {
	a.ChannelType = meta.ChannelType
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	switch meta.ChannelType {
	case channeltype.Azure:
		if meta.Mode == relaymode.ImagesGenerations {
			// https://learn.microsoft.com/en-us/azure/ai-services/openai/dall-e-quickstart?tabs=dalle3%2Ccommand-line&pivots=rest-api
			// https://{resource_name}.openai.azure.com/openai/deployments/dall-e-3/images/generations?api-version=2024-03-01-preview
			fullRequestURL := fmt.Sprintf("%s/openai/deployments/%s/images/generations?api-version=%s", meta.BaseURL, meta.ActualModelName, meta.Config.APIVersion)
			return fullRequestURL, nil
		}

		// https://learn.microsoft.com/en-us/azure/cognitive-services/openai/chatgpt-quickstart?pivots=rest-api&tabs=command-line#rest-api
		requestURL := strings.Split(meta.RequestURLPath, "?")[0]
		requestURL = fmt.Sprintf("%s?api-version=%s", requestURL, meta.Config.APIVersion)
		task := strings.TrimPrefix(requestURL, "/v1/")
		model_ := meta.ActualModelName
		model_ = strings.Replace(model_, ".", "", -1)
		//https://github.com/songquanpeng/one-api/issues/1191
		// {your endpoint}/openai/deployments/{your azure_model}/chat/completions?api-version={api_version}
		requestURL = fmt.Sprintf("/openai/deployments/%s/%s", model_, task)
		return GetFullRequestURL(meta.BaseURL, requestURL, meta.ChannelType), nil
	case channeltype.Minimax:
		return minimax.GetRequestURL(meta)
	case channeltype.Doubao:
		return doubao.GetRequestURL(meta)
	default:

		// 带/chat/completions的接口
		// return GetFullRequestURL(meta.BaseURL, meta.RequestURLPath, meta.ChannelType), nil

		// 自定义和openai中需要填写完整的请求URL。如https://xx.xx/webapi/chat/openai, https://xx.xx/api/openai/v1/chat/completions
		return GetFullRequestURL(meta.BaseURL, meta.RequestURLPath, meta.ChannelType), nil



		
		
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Request, meta *meta.Meta) error {
	adaptor.SetupCommonRequestHeader(c, req, meta)
	if meta.ChannelType == channeltype.Azure {
		req.Header.Set("api-key", meta.APIKey)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+meta.APIKey)
	req.Header.Set("X-Forwarded-For", "127.0.0.1")

	err := godotenv.Load()
    if err != nil {
        log.Fatalf("加载 .env 文件失败: %v", err)
    }
	var Xlobechatauth = env.String("Xlobechatauth", "123")
	// lobeAccessCode校验jwt

	req.Header.Set("X-lobe-chat-auth", Xlobechatauth)
	fmt.Printf("X-lobe-chat-auth: %v\n", Xlobechatauth)

	// fmt.Printf("req.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Headerreq.Header,\n")
	// fmt.Printf("req.Header: %v\n", req.Header)
	if meta.ChannelType == channeltype.OpenRouter {
		req.Header.Set("HTTP-Referer", "https://www.reddit.com")
	}
	return nil
}

func (a *Adaptor) ConvertRequest(c *gin.Context, relayMode int, request *model.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) ConvertImageRequest(request *model.ImageRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error) {
	

	// fmt.Printf("返回了修改后的返回包-“relay/adaptor/openai/adaptor.go”\n")
	return adaptor.DoRequestHelper(a, c, meta, requestBody)
	
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	// 这时候请求包和返回包都修改过了，不能使用meta.IsStream做判断
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		// fmt.Printf("contentType为？%s判断为流式判断为流式判断为流式判断为流式判断为流式\n", contentType)
		var responseText string
		err, responseText, usage = StreamHandler(c, resp, meta.Mode)


		if usage == nil || usage.TotalTokens == 0 {
			usage = ResponseText2Usage(responseText, meta.ActualModelName, meta.PromptTokens)

		}
		if usage.TotalTokens != 0 && usage.PromptTokens == 0 { // some channels don't return prompt tokens & completion tokens
			usage.PromptTokens = meta.PromptTokens
			usage.CompletionTokens = usage.TotalTokens - meta.PromptTokens
		}
	} else {
		// fmt.Printf("contentType为？%s判定为非流式, 判定为非流式判定为非流式判定为非流式判定为非流式判定为非流式判定为非流式判定为非流式判定为非流式\n", contentType)
		switch meta.Mode {
		case relaymode.ImagesGenerations:
			err, _ = ImageHandler(c, resp)
		default:
			// fmt.Printf("计算token和推送至客户端\n")
			err, usage = Handler(c, resp, meta.PromptTokens, meta.ActualModelName)
		}

	}
	return
}

func (a *Adaptor) GetModelList() []string {
	_, modelList := GetCompatibleChannelMeta(a.ChannelType)
	return modelList
}

func (a *Adaptor) GetChannelName() string {
	channelName, _ := GetCompatibleChannelMeta(a.ChannelType)
	return channelName
}
