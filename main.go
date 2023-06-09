package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/sashabaranov/go-openai"
)

type MqttChatRequest struct {
	Topic string `json:"topic"`
	Msg   string `json:"msg"`
}

type MqttChatResponse struct {
	Delta    string `json:"delta"`
	Text     string `json:"Text"`
	IsFinish bool   `json:"is_finish"`
	IsError  bool   `json:"is_error"`
}

// 全局mqtt client
var client MQTT.Client

const openaiAPIURL = "https://api.openai.com/v1/chat/completions"

type ChatRequest struct {
	APIKey      string   `json:"api_key"`
	Model       string   `json:"model"`
	Messages    []string `json:"messages"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature float64  `json:"temperature"`
}

var HttpRroxy string

// 定义一个全局锁
var mutex sync.Mutex

var IsEnableProxy = false

// 定义一个Channel 结构是 MqttChatRequest
var ChatRequestChan = make(chan MqttChatRequest)

var ChatCache = make(map[string][]openai.ChatCompletionMessage, 10)

var gpt *openai.Client

func main() {
	url := flag.String("url", "", "support mqtt tcp url , e.g: baidu.com:1883")
	proxy := flag.String("proxy", "", "support http proxy url , e.g: http://192.168.1.1:1080")
	key := flag.String("key", "", "openai api key e.g: sk-xxxxxx")
	flag.Parse()

	// 判断参数是否全部初始化
	if *url == "" || *proxy == "" || *key == "" {
		log.Println("params dont complete , use -h to see help ")
		os.Exit(1)
	}

	MqttInit(*url)
	GPTInit(*proxy, *key)
	ListenMqtt()
	Chat()
	// 防止退出
	select {}
}

func MqttInit(url string) {
	// 创建MQTT客户端连接配置
	opts := MQTT.NewClientOptions()
	opts.AddBroker("tcp://" + url)
	opts.SetClientID("go-mqtt-chat")
	client = MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
}

func GPTInit(proxy string, key string) {
	// gpt 初始化
	config := openai.DefaultConfig(key)
	proxyUrl, err := url.Parse(proxy)

	if err != nil {
		panic(err)
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyUrl),
	}
	config.HTTPClient = &http.Client{
		Transport: transport,
	}
	gpt = openai.NewClientWithConfig(config)
}

func Chat() {
	for req := range ChatRequestChan {
		// 将请求的消息保存到缓存中
		// 判断是否存在这样的topic

		mutex.Lock()
		if _, ok := ChatCache[req.Topic]; !ok {
			msgs := make([]openai.ChatCompletionMessage, 1)
			msgs[0] = openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一个聊天助手。你的名字叫做胖虎",
			}
			// 如果不存在，初始化
			ChatCache[req.Topic] = msgs
		}
		ChatCache[req.Topic] = append(ChatCache[req.Topic], openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: req.Msg,
		})
		mutex.Unlock()
		// 调用 OpenAiAPiRequest 发起 OpenAI 请求
		go OpenAiAPiRequest(req)
	}
}

func ListenMqtt() {

	log.Println("开始监听MQTT")

	// 订阅MQTT的ChatRequest主题， 监听的消息格式为 MqttChatRequest
	// 将消息发送的Channel中
	client.Subscribe("ChatRequest", 0, func(client MQTT.Client, msg MQTT.Message) {
		// 将msg.Payload()转为MqttChatRequest格式
		// 将转换后的MqttChatRequest发送到ChatRequestChan中
		var req MqttChatRequest
		log.Println("收到请求: ", string(msg.Payload()))
		json.Unmarshal(msg.Payload(), &req)
		ChatRequestChan <- req
	})

}

func OpenAiAPiRequest(req MqttChatRequest) {

	chatReq := openai.ChatCompletionRequest{
		Model:     openai.GPT3Dot5Turbo,
		MaxTokens: 800,
		Messages:  ChatCache[req.Topic],
		Stream:    true,
	}
	ctx := context.Background()
	stream, err := gpt.CreateChatCompletionStream(ctx, chatReq)
	if err != nil {
		fmt.Printf("ChatCompletionStream error: %v\n", err)
		PushMqttResponse(req.Topic, MqttChatResponse{
			Delta:   "",
			Text:    "抱歉 我出错了，请再试一次",
			IsError: true,
		})
		return
	}
	defer stream.Close()
	var text string

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println("Stream finished")
			// 保存到缓存中
			mutex.Lock()
			ChatCache[req.Topic] = append(ChatCache[req.Topic], openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: text,
			})
			// 判断是否超过10条缓存，如果超过则移除最早一次的数据
			// 实际判断 21是因为  10条对话时api返回的，1条 system级别提示词
			if len(ChatCache[req.Topic]) > 21 {
				ChatCache[req.Topic] = ChatCache[req.Topic][1:]
			}

			mutex.Unlock()
			PushMqttResponse(req.Topic, MqttChatResponse{
				Delta:    " ",
				Text:     " ",
				IsFinish: true,
				IsError:  false,
			})
			return
		}
		if err != nil {
			fmt.Println("Stream error: ", err)
			PushMqttResponse(req.Topic, MqttChatResponse{
				Delta:   response.Choices[0].Delta.Content,
				Text:    "抱歉,我出错了，请再试一次",
				IsError: true,
			})
			return
		}
		text += response.Choices[0].Delta.Content
		// 将消息发送到MQTT的ChatResponse主题中
		PushMqttResponse(req.Topic, MqttChatResponse{
			Delta:    response.Choices[0].Delta.Content,
			Text:     text,
			IsFinish: false,
			IsError:  false,
		})

		fmt.Println("streaming：", time.Now().Format(time.UnixDate), text)
	}
}

func PushMqttResponse(topic string, res MqttChatResponse) {
	//fmt.Print("发送消息：", topic, res.Text, "\n")
	// 将res转json格式，发送到mqtt的 topic主题中
	jsonStr, _ := json.Marshal(res)
	client.Publish(topic, 0, false, jsonStr)
}
