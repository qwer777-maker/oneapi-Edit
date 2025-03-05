package adaptor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/client"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/relay/meta"
)


// sse 结构体定义在函数外部，供所有函数共享
type ChatCompletionChunk struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	ServiceTier       string   `json:"service_tier"`
	SystemFingerprint string   `json:"system_fingerprint"`
	Choices           []Choice `json:"choices"`
}

type Choice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	Logprobs     *any    `json:"logprobs"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
    Role    string  `json:"role,omitempty"`
    Content *string `json:"content,omitempty"` 
    Refusal *string `json:"refusal,omitempty"`
}


func isStreamRequest(requestBody io.Reader) (bool, io.Reader, error) {
    bodyBytes, err := io.ReadAll(requestBody)
    if err != nil {
        return false, nil, err
    }

    var payload map[string]interface{}
    if err := json.Unmarshal(bodyBytes, &payload); err != nil {
        return false, nil, err
    }

    stream, ok := payload["stream"].(bool)
    if !ok {
        stream = false
    }

    // 重新构造一个新的io.Reader，以便后续使用
    newRequestBody := bytes.NewReader(bodyBytes)

    return stream, newRequestBody, nil
}




func SetupCommonRequestHeader(c *gin.Context, req *http.Request, meta *meta.Meta) {
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("Accept", c.Request.Header.Get("Accept"))
	if meta.IsStream && c.Request.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
}

func DoRequestHelper(a Adaptor, c *gin.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error) {
	fullRequestURL, err := a.GetRequestURL(meta)

	if err != nil {
		return nil, fmt.Errorf("get request url failed: %w", err)
	}
    // 先调用isStreamRequest，获取isStream和新的requestBody
    isStream, newRequestBody, err := isStreamRequest(requestBody)
    if err != nil {
        return nil, fmt.Errorf("parse request body failed: %w", err)
    }

 
	req, err := http.NewRequest(c.Request.Method, fullRequestURL, newRequestBody)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}

	err = a.SetupRequestHeader(c, req, meta)
	// fmt.Printf("common.go64 SetupRequestHeader调用结束\n")

	if err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}

    var resp *http.Response
    // fmt.Printf("meta.isstream的值为: %v\n", meta.IsStream)


    if isStream {
        // ss调用109行的DoRequestStream
        fmt.Printf("未修改请求的isStream的值为%v，代表原始请求要求流响应\n", isStream)
        // fmt.Printf("common.go67 DoRequestStream调用开始\n")
        resp, err = DoRequestStream(meta.ActualModelName, c, req)
        if err != nil {
            return nil, fmt.Errorf("do request failed: %w", err)
        }
    }else{
        fmt.Printf("未修改请求的isStream的值为%v，代表原始请求不要求流响应\n", isStream)
        // fmt.Printf("common.go67 DoRequestBlock调用开始\n")
        resp, err = DoRequestBlock(meta.ActualModelName, c, req)
        if err != nil {
            return nil, fmt.Errorf("do request failed: %w", err)
        }
    }

	// fmt.Printf("请求结束了+lobe的正常返回包也被修改为openai官方一样的了\n")
	
	return resp, nil
}



// 强制设置stream=true并处理流式响应    
func DoRequestStream(modelnameN string, c *gin.Context, req *http.Request) (*http.Response, error) {
    
    // fmt.Printf("进入了common.go的DoRequestStream函数\n")
    var requestBody map[string]interface{}
    bodyBytes, err := io.ReadAll(req.Body)
    if err != nil {
        return nil, err
    }
    req.Body.Close()

    if len(bodyBytes) > 0 {
        if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
            return nil, err
        }
    } else {
        requestBody = make(map[string]interface{})
    }

    requestBody["stream"] = true

    newRequestBodyBytes, err := json.Marshal(requestBody)
    if err != nil {
        return nil, err
    }

    req.Body = io.NopCloser(bytes.NewReader(newRequestBodyBytes))
    req.ContentLength = int64(len(newRequestBodyBytes))
    req.Header.Set("Content-Length", strconv.Itoa(len(newRequestBodyBytes)))

    resp, err := client.HTTPClient.Do(req)
    if err != nil {
        return nil, err
    }

    responseData, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    resp.Body = io.NopCloser(bytes.NewReader(responseData))

    c.Writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
    c.Writer.Header().Set("Cache-Control", "no-cache")
    c.Writer.Header().Set("Connection", "keep-alive")

    flusher, ok := c.Writer.(http.Flusher)
    if !ok {
        return nil, errors.New("streaming unsupported")
    }

    scanner := bufio.NewScanner(resp.Body)
    id := "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10)
    model := modelnameN
    serviceTier := "default"
    systemFingerprint := "fp_06737a9306"

    isFirstChunk := true

    for {
        var event, data string
        for scanner.Scan() {
            line := scanner.Text()
            if line == "" {
                break // 一个事件块结束
            }
            if strings.HasPrefix(line, "event:") {
                event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
            } else if strings.HasPrefix(line, "data:") {
                data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
            }
        }

        if event == "" && data == "" {
            continue
        }

        if event == "stop" {
            // 最后一个数据段，delta为空对象
            chunk := ChatCompletionChunk{
                ID:                id,
                Object:            "chat.completion.chunk",
                Created:           time.Now().Unix(),
                Model:             model,
                ServiceTier:       serviceTier,
                SystemFingerprint: systemFingerprint,
                Choices: []Choice{
                    {
                        Index:        0,
                        Delta:        Delta{}, // delta为空对象
                        Logprobs:     nil,
                        FinishReason: ptrString("stop"),
                    },
                },
            }
            sendChunk(c.Writer, chunk)
            flusher.Flush()
            break
        }

        var content string
        if err := json.Unmarshal([]byte(data), &content); err != nil {
            logger.Error(context.TODO(), fmt.Sprintf("JSON解码错误: %v", err))
            continue
        }

        // 如果不是第一个数据段，并且content为空字符串，则跳过
        if !isFirstChunk && content == "" {
            continue
        }

        var chunk ChatCompletionChunk
        if isFirstChunk {
            emptyContent := "" // 明确设置为空字符串
            chunk = ChatCompletionChunk{
                ID:                id,
                Object:            "chat.completion.chunk",
                Created:           time.Now().Unix(),
                Model:             model,
                ServiceTier:       serviceTier,
                SystemFingerprint: systemFingerprint,
                Choices: []Choice{
                    {
                        Index: 0,
                        Delta: Delta{
                            Role:    "assistant",
                            Content: &emptyContent, // 明确设置为空字符串指针
                        },
                        Logprobs:     nil,
                        FinishReason: nil,
                    },
                },
            }
            isFirstChunk = false
        } else {
            // 后续数据段，只包含content
            contentCopy := content // 避免指针问题
            chunk = ChatCompletionChunk{
                ID:                id,
                Object:            "chat.completion.chunk",
                Created:           time.Now().Unix(),
                Model:             model,
                ServiceTier:       serviceTier,
                SystemFingerprint: systemFingerprint,
                Choices: []Choice{
                    {
                        Index: 0,
                        Delta: Delta{
                            Content: &contentCopy,
                        },
                        Logprobs:     nil,
                        FinishReason: nil,
                    },
                },
            }
        }

        sendChunk(c.Writer, chunk)
        flusher.Flush()
    }

    // 发送[DONE]
    fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
    flusher.Flush()

    resp.Body.Close()
    return resp, nil
}




func ptrString(s string) *string {
    return &s
}

// 辅助函数：发送单个数据片段
func sendChunk(w io.Writer, chunk ChatCompletionChunk) {
	data, err := json.Marshal(chunk)
	if err != nil {
		logger.Error(context.TODO(), fmt.Sprintf("marshal chunk failed: %v", err))
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}






func DoRequestBlock(modelnameN string, c *gin.Context, req *http.Request) (*http.Response, error) {
	// 先读取并暂存原始请求体内容
	var requestBody map[string]interface{}

    bodyBytes, err := io.ReadAll(req.Body)
    if err != nil {
        return nil, err
    }

    req.Body.Close() 

    // 将原始JSON解析为map对象，以便修改其中的 stream 参数
    if len(bodyBytes) > 0 {
        if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
            return nil, err
        }
    } else {
        requestBody = make(map[string]interface{})
    }

    // 强行设置 "stream" 为 false (无论之前是否存在，都覆盖为false)
    requestBody["stream"] = false

    // 序列化回新的 JSON 请求体数据:
	newRequestBodyBytes, err := json.Marshal(requestBody)
	if err != nil {
	    return nil,err 
	}

	req.Body=io.NopCloser(bytes.NewReader(newRequestBodyBytes))
	req.ContentLength=int64(len(newRequestBodyBytes))

	// 更新header中的Content-Length字段：
	req.Header.Set("Content-Length", strconv.Itoa(len(newRequestBodyBytes)))

	resp,err:=client.HTTPClient.Do(req)

	if(err!=nil){
	   return nil , err 
	}

	// fmt.Printf("最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求最终请求\n")
	// fmt.Printf("Request URL: %s\n", req.URL.String())
    // fmt.Printf("Request Headers: %v\n", req.Header)


	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("resp is nil")
	} else{
		/* responseBody现在长这样。
		打算吧返回内容修改为以下格式供其他平台调用{"id":"","object":"","created":0,"model":"","choices":[{"delta":{"role":"assistant","content":"xxxxx"},"index":0,"finish_reason":null}]}
		id: chatcmpl-B67KhVjCRchp0jKv0gHB7FbL0hLIX
		event: text
		data: "Hello! How can I assist you today?"

		id: chatcmpl-B67KhVjCRchp0jKv0gHB7FbL0hLIX
		event: stop
		data: "stop"
		// */
		responseBody, err := io.ReadAll(resp.Body)
        fmt.Printf("DoRequestBlock的正常lobe的responseBody: \n%s\n", responseBody)
        if err != nil {
            return nil, fmt.Errorf("read_response_body_failed: %v", err)
        }
        err = resp.Body.Close()
        if err != nil {
            return nil, fmt.Errorf("close_response_body_failed: %v", err)
        }
        
        // fmt.Printf("准备将responseBody更新为choices版本...\n")
        /* responseBody现在长这样。
        打算吧返回内容修改为以下格式供其他平台调用{"id":"","object":"","created":0,"model":"","choices":[{"delta":{"role":"assistant","content":"xxxxx"},"index":0,"finish_reason":null}]}
        id: chatcmpl-B67KhVjCRchp0jKv0gHB7FbL0hLIX
        event: text
        data: "Hello! How can I assist you today?"
    
        id: chatcmpl-B67KhVjCRchp0jKv0gHB7FbL0hLIX
        event: stop
        data: "stop"
        */
    
        // 提取第一个非 "stop" 的 data 值
        var extractedData string
        scanner := bufio.NewScanner(bytes.NewReader(responseBody))
        for scanner.Scan() {
            line := scanner.Text()
            if strings.HasPrefix(line, "data:") {
                // 去除前缀及两边的空白字符
                content := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

                // 使用json.Unmarshal安全地处理JSON转义字符，得到真正含换行符的数据
                err := json.Unmarshal([]byte(content), &extractedData)
                if err != nil {
                    fmt.Println("JSON解码错误:", err)
                    continue 
                }

                if extractedData != "stop" {
                    break  // 成功解析出有效数据后退出循环
                }
            }
        }

    
        // 构造目标 JSON 结构，并将 extractedData 嵌入到 message.content 的位置
        type Message struct {
            Role    string  `json:"role"`
            Content string  `json:"content"`
            Refusal *string `json:"refusal"`
        }
        type Choice struct {
            Index        int      `json:"index"`
            Message      Message  `json:"message"`
            Logprobs     *any     `json:"logprobs"` // 可设置为 nil
            FinishReason string   `json:"finish_reason"`
        }
        type PromptTokensDetails struct {
            CachedTokens int `json:"cached_tokens"`
            AudioTokens  int `json:"audio_tokens"`
        }
        type CompletionTokensDetails struct {
            ReasoningTokens            int `json:"reasoning_tokens"`
            AudioTokens                int `json:"audio_tokens"`
            AcceptedPredictionTokens   int `json:"accepted_prediction_tokens"`
            RejectedPredictionTokens   int `json:"rejected_prediction_tokens"`
        }
        type Usage struct {
            PromptTokens            int                     `json:"prompt_tokens"`
            CompletionTokens        int                     `json:"completion_tokens"`
            TotalTokens             int                     `json:"total_tokens"`
            PromptTokensDetails     PromptTokensDetails     `json:"prompt_tokens_details"`
            CompletionTokensDetails CompletionTokensDetails `json:"completion_tokens_details"`
        }
        type Result struct {
            Id                string   `json:"id"`
            Object            string   `json:"object"`
            Created           int64    `json:"created"`
            Model             string   `json:"model"`
            Choices           []Choice `json:"choices"`
            Usage             Usage    `json:"usage"`
            // ServiceTier       string   `json:"service_tier"`
            SystemFingerprint string   `json:"system_fingerprint"`
        }
        res := Result{
            Id:      "chatcmpl-B6BBfld6yNw7QXm9tR0xLvDdkBx2q",
            Object:  "chat.completion",
            Created: 1740812671,
            Model:   modelnameN,
            Choices: []Choice{
                {
                    Index: 0,
                    Message: Message{
                        Role:    "assistant",
                        Content: extractedData, // 第一个 data 的内容
                        Refusal: nil,
                    },
                    Logprobs:     nil,
                    FinishReason: "stop",
                },
            },
            Usage: Usage{
                PromptTokens:     1,
                CompletionTokens: 1,
                TotalTokens:      1,
                PromptTokensDetails: PromptTokensDetails{
                    CachedTokens: 0,
                    AudioTokens:  0,
                },
                CompletionTokensDetails: CompletionTokensDetails{
                    ReasoningTokens:          0,
                    AudioTokens:              0,
                    AcceptedPredictionTokens: 0,
                    RejectedPredictionTokens: 0,
                },
            },
            // ServiceTier:       "default",
            SystemFingerprint: "fp_06737a9306",
        }
        

        finalJSON, err := json.Marshal(res)
        if err != nil {
            // 若 marshal 失败，可作相应处理
            fmt.Printf("marshal result failed: %v\n", err)
        } else {
			fmt.Printf("机器人的回答extractedDataextractedDataextractedData: %s\n", extractedData)


            // fmt.Printf("构造完choice后的Final JSON: %s\n", finalJSON)

			
        }

        resp.Body = io.NopCloser(bytes.NewBuffer(finalJSON))
		resp.Header.Set("Content-Type","application/json;charset=utf-8")
		resp.Header.Set("Content-Length",strconv.Itoa(len(finalJSON)))

	}
	_ = req.Body.Close()
	_ = c.Request.Body.Close()
	return resp, nil
}